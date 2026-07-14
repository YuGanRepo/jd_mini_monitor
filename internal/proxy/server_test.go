package proxy

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

func TestProcessRequestLogsWhenNoRuleMatches(t *testing.T) {
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

	logs := server.RecentLogs()
	if len(logs) != 1 {
		t.Fatalf("logs count = %d, want 1", len(logs))
	}
	if logs[0].ActionType != "passthrough" {
		t.Fatalf("action type = %q, want passthrough", logs[0].ActionType)
	}
	if logs[0].Status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", logs[0].Status)
	}
}

