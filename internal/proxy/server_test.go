package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestCaptureRetention(t *testing.T) {
	captureDir := t.TempDir()
	server := New(Config{CaptureDir: captureDir, Logger: log.New(io.Discard, "", 0)})

	for index := 0; index < maxCartviewCaptureFiles+5; index++ {
		name := filepath.Join(captureDir, "cartview-response-"+strconv.Itoa(index+1000)+".json")
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	server.pruneCaptureFiles("cartview-response-*.json", maxCartviewCaptureFiles)
	matches, err := filepath.Glob(filepath.Join(captureDir, "cartview-response-*.json"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != maxCartviewCaptureFiles {
		t.Fatalf("capture count = %d, want %d", len(matches), maxCartviewCaptureFiles)
	}

	jsonlPath := filepath.Join(captureDir, "proxy-requests.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Truncate(jsonlPath, maxJSONLBytes); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}
	server.appendCaptureJSONL("proxy-requests.jsonl", map[string]bool{"ok": true})
	if _, err := os.Stat(jsonlPath + ".1"); err != nil {
		t.Fatalf("rotated JSONL backup missing: %v", err)
	}
}

func TestModifyResponseBodyClearsChunkedEncoding(t *testing.T) {
	response := &http.Response{
		StatusCode:       http.StatusOK,
		Header:           make(http.Header),
		Body:             io.NopCloser(bytes.NewReader([]byte("original"))),
		ContentLength:    -1,
		TransferEncoding: []string{"chunked"},
	}
	response.Header.Set("Transfer-Encoding", "chunked")

	modified, err := modifyResponse(response, rules.Rule{
		Name:   "replace body",
		Action: rules.Action{Type: "modify", Body: `{"ok":true}`},
	})
	if err != nil {
		t.Fatalf("modifyResponse() error = %v", err)
	}
	if len(modified.TransferEncoding) != 0 {
		t.Fatalf("TransferEncoding = %v, want none", modified.TransferEncoding)
	}
	if got := modified.Header.Get("Transfer-Encoding"); got != "" {
		t.Fatalf("Transfer-Encoding header = %q, want empty", got)
	}
	if got := modified.Header.Get("Content-Length"); got != strconv.Itoa(len(`{"ok":true}`)) {
		t.Fatalf("Content-Length = %q", got)
	}
}
