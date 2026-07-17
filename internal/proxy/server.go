package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/notify"
	"mini-proxy/internal/quote"
	"mini-proxy/internal/rules"
	"mini-proxy/internal/sku"
)

// maxRequestLogEntries bounds the in-memory request log ring buffer kept for
// display in the desktop UI.
const maxRequestLogEntries = 200

const (
	maxCartviewCaptureFiles = 100
	maxJSONLBytes           = 10 << 20
	maxJSONLBackups         = 3
)

// RequestLogEntry describes one request observed by the proxy. Matched
// interception rules include RuleName; unmatched requests are logged as
// passthrough/tunnel entries so runtime diagnostics never appear empty.
type RequestLogEntry struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	URL        string    `json:"url"`
	RuleName   string    `json:"ruleName,omitempty"`
	ActionType string    `json:"actionType,omitempty"`
	Status     int       `json:"status,omitempty"`
}

type Config struct {
	Addr         string
	Rules        *rules.Set
	Certs        *cert.Manager
	Logger       *log.Logger
	SKUStore     *sku.Store
	Notifier     *notify.Notifier
	QuoteMatcher QuoteMatcher
	CaptureDir   string
}

type QuoteMatcher interface {
	Match(skuID, name string) (*quote.Match, error)
}

type Server struct {
	addr         string
	rules        *rules.Set
	certs        *cert.Manager
	logger       *log.Logger
	skuStore     *sku.Store
	notifier     *notify.Notifier
	quoteMatcher QuoteMatcher
	captureDir   string
	httpServer   *http.Server
	transport    *http.Transport

	logMu        sync.Mutex
	requestLogs  []RequestLogEntry
	captureMu    sync.Mutex
	skuPersistMu sync.Mutex
	notifierMu   sync.RWMutex
	quoteMu      sync.RWMutex
	quoteRunMu   sync.Mutex
	quoteRunID   uint64
}

func New(config Config) *Server {
	addr := config.Addr
	if addr == "" {
		addr = "127.0.0.1:8899"
	}
	logger := config.Logger
	if logger == nil {
		logger = log.Default()
	}
	skuStore := config.SKUStore
	if skuStore == nil {
		skuStore = sku.NewStore()
	}

	server := &Server{
		addr:         addr,
		rules:        config.Rules,
		certs:        config.Certs,
		logger:       logger,
		skuStore:     skuStore,
		notifier:     config.Notifier,
		quoteMatcher: config.QuoteMatcher,
		captureDir:   config.CaptureDir,
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	server.httpServer = &http.Server{
		Addr:              addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	server.enrichSKUQuotes(skuStore.Snapshot().Entries)
	return server
}

func (server *Server) Addr() string {
	return server.addr
}

func (server *Server) ListenAndServe() error {
	server.logger.Printf("proxy listening on %s", server.addr)
	err := server.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (server *Server) Serve(listener net.Listener) error {
	server.addr = listener.Addr().String()
	server.logger.Printf("proxy listening on %s", server.addr)
	err := server.httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (server *Server) Shutdown(ctx context.Context) error {
	server.transport.CloseIdleConnections()
	return server.httpServer.Shutdown(ctx)
}

func (server *Server) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodConnect {
		server.handleConnect(responseWriter, request)
		return
	}

	response, err := server.processRequest(request)
	if err != nil {
		server.logger.Printf("proxy request failed: %v", err)
		http.Error(responseWriter, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	copyResponse(responseWriter, response)
}

func (server *Server) handleConnect(responseWriter http.ResponseWriter, request *http.Request) {
	hijacker, ok := responseWriter.(http.Hijacker)
	if !ok {
		http.Error(responseWriter, "connection hijacking is not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		server.logger.Printf("hijack failed: %v", err)
		return
	}

	targetHost := ensurePort(request.Host, "443")
	if server.rules == nil || !server.rules.HasHostMatch(targetHost) {
		// Not a rule host: blind TCP tunnel. Not decrypted, not logged as an
		// interception (only the configured rule URLs are intercepted).
		server.tunnel(clientConn, targetHost)
		return
	}

	server.mitm(clientConn, targetHost)
}

func (server *Server) tunnel(clientConn net.Conn, targetHost string) {
	defer clientConn.Close()

	targetConn, err := net.DialTimeout("tcp", targetHost, 15*time.Second)
	if err != nil {
		_, _ = fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		server.logger.Printf("tunnel dial failed for %s: %v", targetHost, err)
		return
	}
	defer targetConn.Close()

	_, _ = fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetConn, clientConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientConn, targetConn)
		done <- struct{}{}
	}()
	<-done
}

func (server *Server) mitm(clientConn net.Conn, targetHost string) {
	defer clientConn.Close()
	if server.certs == nil {
		_, _ = fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		server.logger.Printf("MITM requested for %s without certificate manager", targetHost)
		return
	}

	_, _ = fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	certificate, err := server.certs.CertificateFor(targetHost)
	if err != nil {
		server.logger.Printf("certificate generation failed for %s: %v", targetHost, err)
		return
	}

	tlsConn := tls.Server(clientConn, &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	})
	if err := tlsConn.Handshake(); err != nil {
		server.logger.Printf("TLS handshake failed for %s: %v", targetHost, err)
		return
	}

	reader := bufio.NewReader(tlsConn)
	for {
		request, err := http.ReadRequest(reader)
		if err != nil {
			if err != io.EOF {
				server.logger.Printf("MITM read request failed for %s: %v", targetHost, err)
			}
			return
		}
		if request.Host == "" {
			request.Host = targetHost
		}
		request.URL.Scheme = "https"
		request.URL.Host = request.Host
		request.RequestURI = ""

		response, err := server.processRequest(request)
		if err != nil {
			server.logger.Printf("MITM request failed: %v", err)
			response = errorResponse(request, http.StatusBadGateway, err.Error())
		}
		if err := response.Write(tlsConn); err != nil {
			_ = response.Body.Close()
			server.logger.Printf("MITM write response failed: %v", err)
			return
		}
		_ = response.Body.Close()
		if request.Close || response.Close {
			return
		}
	}
}

func (server *Server) processRequest(request *http.Request) (*http.Response, error) {
	prepareOutboundURL(request)

	var matchedRule *rules.Rule
	if server.rules != nil {
		if rule, ok := server.rules.Match(request); ok {
			matchedRule = rule
			if delay := rule.Delay(); delay > 0 {
				time.Sleep(delay)
			}
			if rule.Action.Type == "mock" || rule.Action.Type == "static" {
				drainRequestBody(request)
				server.logger.Printf("mocking %s %s with rule %q", request.Method, request.URL.String(), rule.Name)
				response := mockResponse(request, *rule)
				server.appendLog(RequestLogEntry{
					Time:       time.Now(),
					Method:     request.Method,
					URL:        request.URL.String(),
					RuleName:   rule.Name,
					ActionType: rule.Action.Type,
					Status:     response.StatusCode,
				})
				return response, nil
			}
		}
	}

	if matchedRule != nil && matchedRule.Action.Type == "modify" && matchedRule.Action.Body != "" {
		request.Header.Del("Accept-Encoding")
	}
	if matchedRule != nil && matchedRule.Extract != "" {
		// Ask the origin for an uncompressed body so extraction can read it.
		request.Header.Del("Accept-Encoding")
	}

	response, err := server.roundTrip(request)
	if err != nil {
		// Only record failures for requests that matched a rule (the configured
		// URLs). Unmatched requests are plain passthrough and not logged.
		if matchedRule != nil {
			server.appendLog(RequestLogEntry{
				Time:       time.Now(),
				Method:     request.Method,
				URL:        request.URL.String(),
				RuleName:   matchedRule.Name,
				ActionType: matchedRule.Action.Type,
				Status:     http.StatusBadGateway,
			})
		}
		return nil, err
	}

	if matchedRule != nil && matchedRule.Extract != "" {
		server.extractFromResponse(matchedRule, response)
	}

	if matchedRule != nil && matchedRule.Action.Type == "modify" {
		server.logger.Printf("modifying %s %s with rule %q", request.Method, request.URL.String(), matchedRule.Name)
		modified, modifyErr := modifyResponse(response, *matchedRule)
		if modifyErr == nil {
			server.appendLog(RequestLogEntry{
				Time:       time.Now(),
				Method:     request.Method,
				URL:        request.URL.String(),
				RuleName:   matchedRule.Name,
				ActionType: matchedRule.Action.Type,
				Status:     modified.StatusCode,
			})
		}
		return modified, modifyErr
	}

	if matchedRule != nil {
		server.appendLog(RequestLogEntry{
			Time:       time.Now(),
			Method:     request.Method,
			URL:        request.URL.String(),
			RuleName:   matchedRule.Name,
			ActionType: matchedRule.Action.Type,
			Status:     response.StatusCode,
		})
	}

	return response, nil
}

func drainRequestBody(request *http.Request) {
	if request == nil || request.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, request.Body)
	_ = request.Body.Close()
}

func (server *Server) appendLog(entry RequestLogEntry) {
	server.logMu.Lock()
	server.requestLogs = append(server.requestLogs, entry)
	if len(server.requestLogs) > maxRequestLogEntries {
		server.requestLogs = server.requestLogs[len(server.requestLogs)-maxRequestLogEntries:]
	}
	server.logMu.Unlock()

	server.appendCaptureJSONL("proxy-requests.jsonl", entry)
}

func (server *Server) appendCaptureJSONL(name string, value any) {
	if server.captureDir == "" {
		return
	}
	if err := os.MkdirAll(server.captureDir, 0o700); err != nil {
		server.logger.Printf("capture: create dir failed: %v", err)
		return
	}
	line, err := json.Marshal(value)
	if err != nil {
		server.logger.Printf("capture: marshal %s failed: %v", name, err)
		return
	}
	path := filepath.Join(server.captureDir, name)
	server.captureMu.Lock()
	defer server.captureMu.Unlock()
	server.rotateCaptureFile(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		server.logger.Printf("capture: open %s failed: %v", path, err)
		return
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		server.logger.Printf("capture: write %s failed: %v", path, err)
	}
}

func (server *Server) writeCaptureFile(prefix string, ext string, data []byte) string {
	if server.captureDir == "" {
		return ""
	}
	if err := os.MkdirAll(server.captureDir, 0o700); err != nil {
		server.logger.Printf("capture: create dir failed: %v", err)
		return ""
	}
	name := fmt.Sprintf("%s-%s.%s", prefix, time.Now().Format("20060102-150405.000000"), ext)
	path := filepath.Join(server.captureDir, name)
	server.captureMu.Lock()
	defer server.captureMu.Unlock()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		server.logger.Printf("capture: write %s failed: %v", path, err)
		return ""
	}
	server.pruneCaptureFiles(fmt.Sprintf("%s-*.%s", prefix, ext), maxCartviewCaptureFiles)
	return path
}

func (server *Server) writeLatestSKUFile() {
	if server.captureDir == "" || server.skuStore == nil {
		return
	}
	snapshot := server.skuStore.Snapshot()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		server.logger.Printf("capture: marshal sku-latest failed: %v", err)
		return
	}
	if err := os.MkdirAll(server.captureDir, 0o700); err != nil {
		server.logger.Printf("capture: create dir failed: %v", err)
		return
	}
	path := filepath.Join(server.captureDir, "sku-latest.json")
	server.captureMu.Lock()
	defer server.captureMu.Unlock()
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		server.logger.Printf("capture: write %s failed: %v", path, err)
	}
}

func (server *Server) rotateCaptureFile(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxJSONLBytes {
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, maxJSONLBackups))
	for index := maxJSONLBackups - 1; index >= 1; index-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, index), fmt.Sprintf("%s.%d", path, index+1))
	}
	_ = os.Rename(path, path+".1")
}

func (server *Server) pruneCaptureFiles(pattern string, keep int) {
	matches, err := filepath.Glob(filepath.Join(server.captureDir, pattern))
	if err != nil || len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, path := range matches[:len(matches)-keep] {
		_ = os.Remove(path)
	}
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RecentLogs returns a snapshot copy of the most recent proxied requests, oldest first.
func (server *Server) RecentLogs() []RequestLogEntry {
	server.logMu.Lock()
	defer server.logMu.Unlock()
	out := make([]RequestLogEntry, len(server.requestLogs))
	copy(out, server.requestLogs)
	return out
}

// SKUSnapshot returns the current parsed SKU list accumulated from intercepted
// responses whose matching rule declared an "extract" target.
func (server *Server) SKUSnapshot() sku.Snapshot {
	return server.skuStore.Snapshot()
}

// ResetSKUList clears the shared store and its persisted snapshot without
// racing an in-flight extraction update.
func (server *Server) ResetSKUList() {
	server.skuPersistMu.Lock()
	defer server.skuPersistMu.Unlock()
	server.skuStore.Reset()
	server.writeLatestSKUFile()
}

func (server *Server) SetNotifier(notifier *notify.Notifier) {
	server.notifierMu.Lock()
	server.notifier = notifier
	server.notifierMu.Unlock()
	server.enrichSKUQuotes(server.skuStore.Snapshot().Entries)
}

func (server *Server) SetQuoteMatcher(matcher QuoteMatcher) {
	server.quoteMu.Lock()
	server.quoteMatcher = matcher
	server.quoteMu.Unlock()
	server.enrichSKUQuotes(server.skuStore.Snapshot().Entries)
}

// notifyPriceChanges pushes a DingTalk notification for the SKUs whose final
// price changed on the latest capture. Delivery runs in its own goroutine so a
// slow or failing webhook never blocks the proxied response. Failures are logged
// only.
func (server *Server) notifyPriceChanges(changed []sku.Entry) {
	server.notifierMu.RLock()
	notifier := server.notifier
	server.notifierMu.RUnlock()
	if notifier == nil || !notifier.Enabled() || len(changed) == 0 {
		return
	}
	reports := make([]notify.Report, 0, len(changed))
	for _, entry := range changed {
		fieldChanges := make([]notify.FieldChange, 0, len(entry.Changes))
		for _, change := range entry.Changes {
			fieldChanges = append(fieldChanges, notify.FieldChange{
				Category: change.Category, Field: change.Field, Old: change.Old, New: change.New,
				Description: change.Description, OldNumber: change.OldNumber, NewNumber: change.NewNumber, Numeric: change.Numeric,
			})
		}
		reports = append(reports, notify.Report{
			ItemID: entry.ItemID, Name: entry.Name, VendorName: entry.VendorName, Num: entry.Num,
			StockDesc: entry.StockDesc, RemainNum: entry.RemainNum,
			PagePriceCents: entry.PagePriceCents, FinalPriceCents: entry.FinalPriceCents,
			ProductURL: entry.ProductURL, CheckoutURL: entry.CheckoutURL, Changes: fieldChanges,
		})
	}
	go func() {
		reports = server.filterReportsByQuote(notifier, reports)
		if len(reports) == 0 {
			server.logger.Printf("notify: all changed SKUs were filtered")
			return
		}
		if err := notifier.NotifyReports(reports); err != nil {
			server.logger.Printf("notify: push failed (%d reports): %v", len(reports), err)
			return
		}
		server.logger.Printf("notify: push sent for %d changed SKU(s)", len(reports))
	}()
}

func (server *Server) filterReportsByQuote(notifier *notify.Notifier, reports []notify.Report) []notify.Report {
	enabled, threshold := notifier.QuoteFilter()
	server.quoteMu.RLock()
	matcher := server.quoteMatcher
	server.quoteMu.RUnlock()
	if matcher == nil {
		return reports
	}
	kept := make([]notify.Report, 0, len(reports))
	for _, report := range reports {
		match, err := matcher.Match(report.ItemID, report.Name)
		if err != nil || match == nil {
			kept = append(kept, report)
			continue
		}
		difference := quote.CalculateDiff(report.Name, report.FinalPriceCents, notifier.DiscountRate(), match)
		if difference == nil {
			kept = append(kept, report)
			continue
		}
		report.HasQuote = true
		report.QuoteName = difference.QuoteName
		report.QuoteSpec = difference.Spec
		report.QuotePricePerUnit = difference.PricePerUnit
		report.QuoteTotal = difference.QuoteTotal
		report.QuoteCost = difference.CostTotal
		report.QuoteDiff = difference.Amount
		report.ProfitRatio = difference.ProfitRatio
		if !enabled || difference.Amount > threshold {
			kept = append(kept, report)
		}
	}
	return kept
}

func (server *Server) enrichSKUQuotes(entries []sku.Entry) {
	server.quoteMu.RLock()
	matcher := server.quoteMatcher
	server.quoteMu.RUnlock()
	if matcher == nil || len(entries) == 0 {
		return
	}
	server.notifierMu.RLock()
	notifier := server.notifier
	server.notifierMu.RUnlock()
	discountRate := 1.0
	quoteNotifyEnabled := false
	quoteThreshold := 0.0
	if notifier != nil {
		discountRate = notifier.DiscountRate()
		quoteNotifyEnabled, quoteThreshold = notifier.QuoteFilter()
		quoteNotifyEnabled = quoteNotifyEnabled && notifier.Enabled()
	}

	server.quoteRunMu.Lock()
	server.quoteRunID++
	runID := server.quoteRunID
	server.quoteRunMu.Unlock()

	go func() {
		semaphore := make(chan struct{}, 6)
		var wait sync.WaitGroup
		var reportMu sync.Mutex
		quoteReports := make([]notify.Report, 0)
		claimedKeys := make(map[string]string)
		for _, entry := range entries {
			entry := entry
			wait.Add(1)
			go func() {
				defer wait.Done()
				semaphore <- struct{}{}
				match, err := matcher.Match(entry.ItemID, entry.Name)
				<-semaphore

				result := sku.QuoteResult{}
				switch {
				case err != nil:
					result.Status = sku.QuoteStatusError
					result.Error = err.Error()
				case match == nil:
					result.Status = sku.QuoteStatusUnmatched
				default:
					difference := quote.CalculateDiff(entry.Name, entry.FinalPriceCents, discountRate, match)
					if difference == nil {
						result.Status = sku.QuoteStatusUnmatched
					} else {
						result.Status = sku.QuoteStatusMatched
						result.Name = difference.QuoteName
						result.Spec = difference.Spec
						result.Price = difference.PricePerUnit
						result.Total = difference.QuoteTotal
						result.Cost = difference.CostTotal
						result.Diff = difference.Amount
						result.ProfitRate = difference.ProfitRatio
					}
				}
				server.quoteRunMu.Lock()
				current := server.quoteRunID == runID
				server.quoteRunMu.Unlock()
				if current {
					server.skuStore.ApplyQuote(entry.ItemID, entry.FinalPriceCents, result)
					if quoteNotifyEnabled && len(entry.Changes) == 0 && result.Status == sku.QuoteStatusMatched && result.Diff > quoteThreshold {
						key := fmt.Sprintf("%d|%.4f|%.4f|%.4f|%s", entry.FinalPriceCents, result.Total, result.Cost, result.Diff, result.Name)
						if server.skuStore.ClaimQuoteNotification(entry.ItemID, entry.FinalPriceCents, key) {
							reportMu.Lock()
							claimedKeys[entry.ItemID] = key
							quoteReports = append(quoteReports, notify.Report{
								ItemID: entry.ItemID, Name: entry.Name, VendorName: entry.VendorName, Num: entry.Num,
								StockDesc: entry.StockDesc, RemainNum: entry.RemainNum,
								PagePriceCents: entry.PagePriceCents, FinalPriceCents: entry.FinalPriceCents,
								ProductURL: entry.ProductURL, CheckoutURL: entry.CheckoutURL,
								HasQuote: true, QuoteTriggered: true, QuoteName: result.Name, QuoteSpec: result.Spec,
								QuotePricePerUnit: result.Price, QuoteTotal: result.Total, QuoteCost: result.Cost,
								QuoteDiff: result.Diff, ProfitRatio: result.ProfitRate,
							})
							reportMu.Unlock()
						}
					}
				}
			}()
		}
		wait.Wait()
		server.quoteRunMu.Lock()
		current := server.quoteRunID == runID
		server.quoteRunMu.Unlock()
		if current {
			if len(quoteReports) > 0 {
				if err := notifier.NotifyReports(quoteReports); err != nil {
					for itemID, key := range claimedKeys {
						server.skuStore.ReleaseQuoteNotification(itemID, key)
					}
					server.logger.Printf("notify: quote push failed (%d reports): %v", len(quoteReports), err)
				} else {
					server.logger.Printf("notify: quote push sent for %d SKU(s)", len(quoteReports))
				}
			}
			server.skuPersistMu.Lock()
			server.writeLatestSKUFile()
			server.skuPersistMu.Unlock()
		}
	}()
}

// setBufferedBody replaces a response body with an in-memory buffer and fixes up
// the framing so the rewritten response is sent with a definite Content-Length
// instead of chunked transfer-encoding.
//
// This matters because extraction strips the request's Accept-Encoding, which
// makes Go's transport transparently gunzip the response and drop its
// Content-Length (leaving ContentLength = -1). Re-sending such a response would
// use chunked encoding, and some clients — notably the JD mini-program's HTTP
// stack — fail to parse the chunked cart body and render an empty cart. Setting
// an explicit Content-Length keeps the response well-formed for every client.
func setBufferedBody(response *http.Response, body []byte) {
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.TransferEncoding = nil
	response.Header.Del("Transfer-Encoding")
	response.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

// extractFromResponse buffers the response body (so it can still be forwarded to
// the client), decodes it if gzip-compressed, and feeds it to the SKU store
// according to the rule's Extract target. Failures are logged and never affect
// the response returned to the client.
func (server *Server) extractFromResponse(rule *rules.Rule, response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	raw, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		setBufferedBody(response, nil)
		server.logger.Printf("extract: read body failed for rule %q: %v", rule.Name, err)
		return
	}
	// Restore the untouched body for the client.
	setBufferedBody(response, raw)

	payload := raw
	// Auto-detect gzip: trust Content-Encoding first, then magic bytes.
	isGzip := strings.Contains(strings.ToLower(response.Header.Get("Content-Encoding")), "gzip") ||
		(len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b)
	if isGzip {
		if decoded, decErr := gunzip(raw); decErr == nil {
			payload = decoded
		} else {
			server.logger.Printf("extract: gunzip failed for rule %q (len=%d): %v", rule.Name, len(raw), decErr)
			return
		}
	}

	switch rule.Extract {
	case "jd-cartview":
		payloadPath := server.writeCaptureFile("cartview-response", "json", payload)
		cartview, parseErr := sku.ParseCartviewSnapshot(payload)
		if parseErr != nil {
			server.appendCaptureJSONL("sku-events.jsonl", map[string]any{
				"time":         time.Now(),
				"rule":         rule.Name,
				"payloadPath":  payloadPath,
				"payloadBytes": len(payload),
				"parsed":       0,
				"error":        parseErr.Error(),
			})
			server.logger.Printf("extract: parse cartview failed for rule %q (len=%d, file=%s): %v", rule.Name, len(payload), payloadPath, parseErr)
			return
		}
		if !cartview.Authoritative {
			server.appendCaptureJSONL("sku-events.jsonl", map[string]any{
				"time":         time.Now(),
				"rule":         rule.Name,
				"payloadPath":  payloadPath,
				"payloadBytes": len(payload),
				"parsed":       len(cartview.SKUs),
				"ignored":      true,
				"reason":       "filtered cart snapshot",
			})
			server.logger.Printf("extract: rule=%q ignored filtered cart snapshot parsed=%d file=%s", rule.Name, len(cartview.SKUs), payloadPath)
			return
		}
		skus := cartview.SKUs
		server.skuPersistMu.Lock()
		result := server.skuStore.Update(skus)
		server.writeLatestSKUFile()
		server.skuPersistMu.Unlock()
		server.enrichSKUQuotes(server.skuStore.Snapshot().Entries)
		server.notifyPriceChanges(result.ChangedEntries)
		server.appendCaptureJSONL("sku-events.jsonl", map[string]any{
			"time":         time.Now(),
			"rule":         rule.Name,
			"payloadPath":  payloadPath,
			"payloadBytes": len(payload),
			"parsed":       result.Parsed,
			"changed":      result.Changed,
			"total":        result.Total,
		})
		server.logger.Printf("extract: rule=%q parsed=%d changed=%d total=%d file=%s", rule.Name, result.Parsed, result.Changed, result.Total, payloadPath)
	default:
		server.logger.Printf("extract: unknown target %q for rule %q", rule.Extract, rule.Name)
	}
}

func gunzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (server *Server) roundTrip(request *http.Request) (*http.Response, error) {
	outbound := request.Clone(request.Context())
	outbound.RequestURI = ""
	outbound.Header = request.Header.Clone()
	removeHopByHopHeaders(outbound.Header)
	outbound.Body = request.Body
	outbound.Close = false
	return server.transport.RoundTrip(outbound)
}

func mockResponse(request *http.Request, rule rules.Rule) *http.Response {
	body := []byte(rule.Action.Body)
	status := rule.Action.Status
	if status == 0 {
		status = http.StatusOK
	}

	header := make(http.Header)
	for key, value := range rule.Action.Headers {
		header.Set(key, value)
	}
	if rule.Action.ContentType != "" {
		header.Set("Content-Type", rule.Action.ContentType)
	} else if header.Get("Content-Type") == "" {
		header.Set("Content-Type", inferContentType(body))
	}
	header.Set("X-Mini-Proxy-Rule", rule.Name)
	header.Set("Content-Length", strconv.Itoa(len(body)))

	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}

func modifyResponse(response *http.Response, rule rules.Rule) (*http.Response, error) {
	for key, value := range rule.Action.Headers {
		response.Header.Set(key, value)
	}
	if rule.Action.Status != 0 {
		response.StatusCode = rule.Action.Status
		response.Status = fmt.Sprintf("%d %s", rule.Action.Status, http.StatusText(rule.Action.Status))
	}
	if rule.Action.Body != "" {
		_ = response.Body.Close()
		body := []byte(rule.Action.Body)
		setBufferedBody(response, body)
		response.Header.Del("Content-Encoding")
		if rule.Action.ContentType != "" {
			response.Header.Set("Content-Type", rule.Action.ContentType)
		} else if response.Header.Get("Content-Type") == "" {
			response.Header.Set("Content-Type", inferContentType(body))
		}
	}
	response.Header.Set("X-Mini-Proxy-Rule", rule.Name)
	return response, nil
}

func errorResponse(request *http.Request, status int, message string) *http.Response {
	body := []byte(message)
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}, "Content-Length": []string{strconv.Itoa(len(body))}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}

func copyResponse(responseWriter http.ResponseWriter, response *http.Response) {
	removeHopByHopHeaders(response.Header)
	for key, values := range response.Header {
		for _, value := range values {
			responseWriter.Header().Add(key, value)
		}
	}
	responseWriter.WriteHeader(response.StatusCode)
	_, _ = io.Copy(responseWriter, response.Body)
}

func prepareOutboundURL(request *http.Request) {
	if request.URL.Scheme == "" {
		request.URL.Scheme = "http"
	}
	if request.URL.Host == "" {
		request.URL.Host = request.Host
	}
}

func ensurePort(host string, defaultPort string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host + ":" + defaultPort
	}
	if strings.Count(host, ":") > 1 {
		return net.JoinHostPort(host, defaultPort)
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), defaultPort)
}

func removeHopByHopHeaders(header http.Header) {
	for _, key := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(key)
	}
}

func inferContentType(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if json.Valid(trimmed) {
		return "application/json; charset=utf-8"
	}
	if len(trimmed) == 0 {
		return "text/plain; charset=utf-8"
	}
	return http.DetectContentType(body)
}
