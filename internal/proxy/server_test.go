package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"mini-proxy/internal/rules"
	"mini-proxy/internal/sku"
)

func TestProcessRequestLogsPassthroughMatch(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	ruleSet, err := rules.NewSet([]rules.Rule{
		{
			Name:     "passthrough test",
			Host:     "example.test",
			Method:   http.MethodGet,
			Path:     "/client.action/deal/mshopcart/cartview",
			Extract:  "jd-cartview",
			Priority: 100,
			Action:   rules.Action{Type: "passthrough"},
		},
	}, ".")
	if err != nil {
		t.Fatalf("rules.NewSet() error = %v", err)
	}

	server := New(Config{Rules: ruleSet, Logger: log.New(io.Discard, "", 0), SKUStore: sku.NewStore()})
	server.transport = &http.Transport{Proxy: nil}
	defer server.transport.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodGet, backend.URL+"/client.action/deal/mshopcart/cartview", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Host = "example.test"

	response, err := server.processRequest(request)
	if err != nil {
		t.Fatalf("processRequest() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	logs := server.RecentLogs()
	if len(logs) != 1 {
		t.Fatalf("logs count = %d, want 1", len(logs))
	}
	if logs[0].ActionType != "passthrough" {
		t.Fatalf("action type = %q, want passthrough", logs[0].ActionType)
	}
	if logs[0].Status != http.StatusOK {
		t.Fatalf("logged status = %d, want 200", logs[0].Status)
	}
}

// TestExtractResponseHasDefiniteContentLength verifies that an intercepted +
// extracted response is rewritten with an explicit Content-Length (and no
// chunked transfer-encoding) even when the origin gzips it. The JD mini-program
// cart fails to render a chunked body, so this framing must stay definite.
func TestExtractResponseHasDefiniteContentLength(t *testing.T) {
	body := []byte(`{"cartInfo":{"flatSkus":{"1":{"itemId":"1","itemName":"A","price":"10.00","itemNum":1,"vendorId":"9"}}}}`)

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Go's transport re-adds Accept-Encoding: gzip after we strip the
		// client's, so respond gzipped like the real origin does.
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		_, _ = gz.Write(body)
		_ = gz.Close()
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Encoding", "gzip")
		writer.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
		_, _ = writer.Write(compressed.Bytes())
	}))
	defer backend.Close()

	ruleSet, err := rules.NewSet([]rules.Rule{
		{
			Name:     "extract cartview",
			Host:     "example.test",
			Method:   http.MethodGet,
			Path:     "/client.action/deal/mshopcart/cartview",
			Extract:  "jd-cartview",
			Priority: 100,
			Action:   rules.Action{Type: "passthrough"},
		},
	}, ".")
	if err != nil {
		t.Fatalf("rules.NewSet() error = %v", err)
	}

	server := New(Config{Rules: ruleSet, Logger: log.New(io.Discard, "", 0), SKUStore: sku.NewStore()})
	defer server.transport.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodGet, backend.URL+"/client.action/deal/mshopcart/cartview", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Host = "example.test"

	response, err := server.processRequest(request)
	if err != nil {
		t.Fatalf("processRequest() error = %v", err)
	}
	defer response.Body.Close()

	if len(response.TransferEncoding) != 0 {
		t.Fatalf("response uses transfer-encoding %v, want none (definite length)", response.TransferEncoding)
	}

	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body error = %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body = %q, want decompressed JSON %q", got, body)
	}
	if response.ContentLength != int64(len(body)) {
		t.Fatalf("ContentLength = %d, want %d", response.ContentLength, len(body))
	}
	if header := response.Header.Get("Content-Length"); header != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length header = %q, want %d", header, len(body))
	}
}

func TestProcessRequestDoesNotLogUnmatched(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	server := New(Config{Logger: log.New(io.Discard, "", 0), SKUStore: sku.NewStore()})
	server.transport = &http.Transport{Proxy: nil}
	defer server.transport.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodGet, backend.URL+"/not-matched", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	response, err := server.processRequest(request)
	if err != nil {
		t.Fatalf("processRequest() error = %v", err)
	}
	defer response.Body.Close()

	// Unmatched requests are plain passthrough: not recorded in the request log.
	if logs := server.RecentLogs(); len(logs) != 0 {
		t.Fatalf("logs count = %d, want 0", len(logs))
	}
}
