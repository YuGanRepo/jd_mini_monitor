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
	"strconv"
	"strings"
	"sync"
	"time"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/rules"
	"mini-proxy/internal/sku"
)

// maxRequestLogEntries bounds the in-memory request log ring buffer kept for
// display in the desktop UI.
const maxRequestLogEntries = 200

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
	Addr       string
	Rules      *rules.Set
	Certs      *cert.Manager
	Logger     *log.Logger
	SKUStore   *sku.Store
	CaptureDir string
}

type Server struct {
	addr       string
	rules      *rules.Set
	certs      *cert.Manager
	logger     *log.Logger
	skuStore   *sku.Store
	captureDir string
	httpServer *http.Server
	transport  *http.Transport

	logMu       sync.Mutex
	requestLogs []RequestLogEntry
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
		addr:       addr,
		rules:      config.Rules,
		certs:      config.Certs,
		logger:     logger,
		skuStore:   skuStore,
		captureDir: config.CaptureDir,
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
		server.appendLog(RequestLogEntry{
			Time:       time.Now(),
			Method:     http.MethodConnect,
			URL:        targetHost,
			ActionType: "tunnel",
			Status:     http.StatusOK,
		})
		server.tunnel(clientConn, targetHost)
		return
	}

	server.appendLog(RequestLogEntry{
		Time:       time.Now(),
		Method:     http.MethodConnect,
		URL:        targetHost,
		ActionType: "mitm",
		Status:     http.StatusOK,
	})

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
		server.appendLog(RequestLogEntry{
			Time:       time.Now(),
			Method:     request.Method,
			URL:        request.URL.String(),
			RuleName:   matchedRuleName(matchedRule),
			ActionType: matchedActionType(matchedRule),
			Status:     http.StatusBadGateway,
		})
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
	} else {
		server.appendLog(RequestLogEntry{
			Time:       time.Now(),
			Method:     request.Method,
			URL:        request.URL.String(),
			ActionType: "passthrough",
			Status:     response.StatusCode,
		})
	}

	return response, nil
}

func matchedRuleName(rule *rules.Rule) string {
	if rule == nil {
		return ""
	}
	return rule.Name
}

func matchedActionType(rule *rules.Rule) string {
	if rule == nil {
		return "error"
	}
	return rule.Action.Type
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
	if err := os.WriteFile(path, data, 0o600); err != nil {
		server.logger.Printf("capture: write %s failed: %v", path, err)
		return ""
	}
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
	if err := os.WriteFile(path, data, 0o600); err != nil {
		server.logger.Printf("capture: write %s failed: %v", path, err)
	}
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
		response.Body = io.NopCloser(bytes.NewReader(nil))
		server.logger.Printf("extract: read body failed for rule %q: %v", rule.Name, err)
		return
	}
	// Restore the untouched body for the client.
	response.Body = io.NopCloser(bytes.NewReader(raw))

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
		skus, parseErr := sku.ParseCartview(payload)
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
		result := server.skuStore.Update(skus)
		server.writeLatestSKUFile()
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
		response.Body = io.NopCloser(bytes.NewReader(body))
		response.ContentLength = int64(len(body))
		response.Header.Set("Content-Length", strconv.Itoa(len(body)))
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
