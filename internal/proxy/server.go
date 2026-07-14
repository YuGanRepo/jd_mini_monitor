package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/rules"
)

// maxRequestLogEntries bounds the in-memory request log ring buffer kept for
// display in the desktop UI.
const maxRequestLogEntries = 200

// RequestLogEntry describes one request that matched an interception rule
// (mock/static/modify). Passthrough requests that did not match any rule are
// not recorded.
type RequestLogEntry struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	URL        string    `json:"url"`
	RuleName   string    `json:"ruleName,omitempty"`
	ActionType string    `json:"actionType,omitempty"`
	Status     int       `json:"status,omitempty"`
}

type Config struct {
	Addr   string
	Rules  *rules.Set
	Certs  *cert.Manager
	Logger *log.Logger
}

type Server struct {
	addr       string
	rules      *rules.Set
	certs      *cert.Manager
	logger     *log.Logger
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

	server := &Server{
		addr:   addr,
		rules:  config.Rules,
		certs:  config.Certs,
		logger: logger,
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

	response, err := server.roundTrip(request)
	if err != nil {
		return nil, err
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

	return response, nil
}

func (server *Server) appendLog(entry RequestLogEntry) {
	server.logMu.Lock()
	defer server.logMu.Unlock()
	server.requestLogs = append(server.requestLogs, entry)
	if len(server.requestLogs) > maxRequestLogEntries {
		server.requestLogs = server.requestLogs[len(server.requestLogs)-maxRequestLogEntries:]
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
