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
	if result.QuoteTotal != 108 || result.CostTotal < 58.19 || result.CostTotal > 58.21 {
		t.Fatalf("quote/cost totals = %.2f/%.2f, want 108/58.2", result.QuoteTotal, result.CostTotal)
	}
	if PackageDivisor("白酒12瓶装") != 12 || PackageDivisor("单瓶") != 1 {
		t.Fatal("PackageDivisor() mismatch")
	}
}

func TestCalculateDiffDisablesInvalidDiscountRate(t *testing.T) {
	match := &Match{Name: "报价", SinglePrice: 20}
	for _, rate := range []float64{0, 1, 1.2} {
		result := CalculateDiff("单瓶", 1000, rate, match)
		if result == nil || result.CostTotal != 10 || result.Amount != 10 {
			t.Fatalf("CalculateDiff(rate=%v) = %+v, want cost/diff 10", rate, result)
		}
	}
}
