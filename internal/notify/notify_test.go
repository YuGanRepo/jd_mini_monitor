package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscountCents(t *testing.T) {
	cases := []struct {
		rate  float64
		final int64
		want  int64
	}{
		{0, 10000, 10000},    // disabled
		{1, 10000, 10000},    // disabled
		{1.5, 10000, 10000},  // out of range -> disabled
		{0.95, 10000, 9500},  // 95折
		{0.9, 9999, 8999},    // rounding (8999.1 -> 8999)
		{0.88, 12345, 10864}, // 10863.6 -> 10864
	}
	for _, c := range cases {
		n, err := New(Config{DiscountRate: c.rate}, nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := n.discountCents(c.final); got != c.want {
			t.Errorf("discountCents(rate=%v, final=%d) = %d, want %d", c.rate, c.final, got, c.want)
		}
	}
}

func TestBuildMessageDefaultTemplate(t *testing.T) {
	n, err := New(Config{DiscountRate: 0.9, Format: FormatText}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, err := n.BuildMessage([]Change{{
		ItemID:     "123",
		Name:       "商品A",
		StockDesc:  "有货",
		FinalCents: 9000,
		PrevCents:  10000,
		DeltaCents: -1000,
	}})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	for _, want := range []string{"商品A", "SKU: 123", "90.00", "原价 ¥100.00", "降 -10.00", "折后价: ¥81.00"} {
		if !strings.Contains(body, want) {
			t.Errorf("message missing %q\n---\n%s", want, body)
		}
	}
}

func TestBuildMessageCustomTemplate(t *testing.T) {
	n, err := New(Config{Template: "{{.Name}}={{.FinalYuan}}"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, err := n.BuildMessage([]Change{
		{Name: "A", FinalCents: 100},
		{Name: "B", FinalCents: 250},
	})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	if body != "A=1.00\nB=2.50" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestInvalidTemplate(t *testing.T) {
	if _, err := New(Config{Template: "{{.Name"}, nil); err == nil {
		t.Fatal("expected template parse error")
	}
}

func TestSignedURL(t *testing.T) {
	n, err := New(Config{DingTalk: DingTalkConfig{
		WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=abc",
		Secret:     "SEC123",
	}}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	n.now = func() time.Time { return time.UnixMilli(1700000000000) }
	got, err := n.signedURL()
	if err != nil {
		t.Fatalf("signedURL: %v", err)
	}
	if !strings.Contains(got, "timestamp=1700000000000") {
		t.Errorf("missing timestamp: %s", got)
	}
	if !strings.Contains(got, "sign=") {
		t.Errorf("missing sign: %s", got)
	}
	if !strings.Contains(got, "access_token=abc") {
		t.Errorf("dropped original query: %s", got)
	}
}

func TestSignedURLNoSecret(t *testing.T) {
	raw := "https://oapi.dingtalk.com/robot/send?access_token=abc"
	n, _ := New(Config{DingTalk: DingTalkConfig{WebhookURL: raw}}, nil)
	got, err := n.signedURL()
	if err != nil {
		t.Fatalf("signedURL: %v", err)
	}
	if got != raw {
		t.Errorf("url changed without secret: %s", got)
	}
}

func TestNotifyDisabledIsNoop(t *testing.T) {
	n, _ := New(Config{Enabled: false}, nil)
	if err := n.Notify([]Change{{Name: "x"}}); err != nil {
		t.Fatalf("disabled Notify should be nil, got %v", err)
	}
}

func TestNotifyPostsTextPayload(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	n, err := New(Config{
		Enabled:      true,
		Format:       FormatText,
		DiscountRate: 0.9,
		DingTalk:     DingTalkConfig{WebhookURL: server.URL},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := n.Notify([]Change{{ItemID: "1", Name: "商品", FinalCents: 1000, PrevCents: 2000, DeltaCents: -1000, StockDesc: "有货"}}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if received["msgtype"] != "text" {
		t.Fatalf("expected msgtype text, got %v", received["msgtype"])
	}
}

func TestNotifyReportsErrcode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"sign not match"}`))
	}))
	defer server.Close()

	n, _ := New(Config{Enabled: true, DingTalk: DingTalkConfig{WebhookURL: server.URL}}, nil)
	err := n.Notify([]Change{{Name: "x", FinalCents: 100}})
	if err == nil || !strings.Contains(err.Error(), "310000") {
		t.Fatalf("expected errcode error, got %v", err)
	}
}
