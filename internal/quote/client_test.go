package quote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientMatchAndCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		var body map[string]string
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["sku"] != "100" || body["key"] != "KEY" || body["deviceId"] != "DEVICE" {
			t.Fatalf("unexpected request: %+v", body)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "match": map[string]any{"name": "报价", "singlePrice": 20, "casePerUnit": 18}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "KEY", "DEVICE")
	now := time.Now()
	client.now = func() time.Time { return now }
	first, err := client.Match("100", "商品")
	if err != nil || first == nil || first.SinglePrice != 20 {
		t.Fatalf("first Match() = %+v, %v", first, err)
	}
	second, err := client.Match("100", "商品")
	if err != nil || second == nil || calls != 1 {
		t.Fatalf("cached Match() = %+v, %v, calls=%d", second, err, calls)
	}
	client.now = func() time.Time { return now.Add(cacheTTL + time.Second) }
	if _, err := client.Match("100", "商品"); err != nil || calls != 2 {
		t.Fatalf("expired cache calls=%d err=%v", calls, err)
	}
}

func TestClientDoesNotCacheNetworkFailure(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "KEY", "DEVICE")
	if _, err := client.Match("100", "商品"); err == nil {
		t.Fatal("expected network error")
	}
	if len(client.cache) != 0 {
		t.Fatal("network failure was cached")
	}
}

func TestCalculateDiffMatchesPackageSemantics(t *testing.T) {
	match := &Match{Name: "报价", SinglePrice: 20, CasePerUnit: 18}
	result := CalculateDiff("白酒*6整箱", 6000, 0.97, match)
	if result == nil || result.Spec != "原箱" {
		t.Fatalf("CalculateDiff() = %+v", result)
	}
	// Cost is 60*0.97=58.2 total; quote is 18*6=108; diff=49.8.
	if result.Amount < 49.79 || result.Amount > 49.81 {
		t.Fatalf("diff = %f, want 49.8", result.Amount)
	}
	if PackageDivisor("白酒12瓶装") != 12 || PackageDivisor("单瓶") != 1 {
		t.Fatal("PackageDivisor() mismatch")
	}
}
