package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestDingTalkExplicitDisable(t *testing.T) {
	legacy, _ := New(Config{DingTalk: DingTalkConfig{WebhookURL: "https://example.invalid"}}, nil)
	if !legacy.dingTalkEnabled() {
		t.Fatal("missing legacy enabled flag should default to enabled")
	}
	disabled, _ := New(Config{DingTalk: DingTalkConfig{Enabled: Bool(false), WebhookURL: "https://example.invalid"}}, nil)
	if disabled.dingTalkEnabled() {
		t.Fatal("explicit false should disable DingTalk")
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

func TestNotifyReportsSendsQuoteOnlyReportWithTitleInBody(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&received)
		_, _ = writer.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	notifier, err := New(Config{
		Enabled:  true,
		Format:   FormatMarkdown,
		Title:    "京东小程序通知",
		DingTalk: DingTalkConfig{Enabled: Bool(true), WebhookURL: server.URL},
	}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	report := Report{
		ItemID: "1", Name: "商品", FinalPriceCents: 1000,
		HasQuote: true, QuoteTriggered: true, QuoteName: "系统行情", QuoteSpec: "单瓶",
		QuotePricePerUnit: 20, QuoteTotal: 20, QuoteCost: 9, QuoteDiff: 11,
	}
	if err := notifier.NotifyReports([]Report{report}); err != nil {
		t.Fatalf("NotifyReports() error = %v", err)
	}
	markdown, _ := received["markdown"].(map[string]any)
	body, _ := markdown["text"].(string)
	if !strings.Contains(body, "京东小程序通知") || !strings.Contains(body, "报价对比") || !strings.Contains(body, "差价：+11.00") {
		t.Fatalf("quote-only markdown body missing title or quote details: %s", body)
	}
}

func TestNotifyReportsDoesNotBypassDisabledCategoryForAttachedQuote(t *testing.T) {
	notifier, err := New(Config{Categories: &CategoryConfig{}}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	report := Report{
		ItemID: "1", Name: "商品", HasQuote: true,
		Changes: []FieldChange{{Category: "price", Field: "到手价"}},
	}
	report.Changes = notifier.filterChanges(report.Changes)
	if len(report.Changes) != 0 || report.QuoteTriggered {
		t.Fatalf("test precondition failed: %+v", report)
	}
	filtered := make([]Report, 0, 1)
	if len(report.Changes) > 0 || report.QuoteTriggered {
		filtered = append(filtered, report)
	}
	if len(filtered) != 0 {
		t.Fatalf("attached quote bypassed disabled category: %+v", filtered)
	}
}

func TestReportFilteringMatchesPluginSemantics(t *testing.T) {
	notifier, err := New(Config{
		Categories:           &CategoryConfig{Price: true, Stock: true, Promo: false, Gift: false},
		StockChangeThreshold: 5,
	}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	filtered := notifier.filterChanges([]FieldChange{
		{Category: "price", Field: "到手价"},
		{Category: "promo", Field: "新增促销"},
		{Category: "gift", Field: "新增赠品"},
		{Category: "stock", Field: "剩余库存", Numeric: true, OldNumber: 10, NewNumber: 5},
		{Category: "stock", Field: "剩余库存", Numeric: true, OldNumber: 10, NewNumber: 4},
		{Category: "stock", Field: "库存状态"},
	})
	if len(filtered) != 3 {
		t.Fatalf("filtered changes = %+v, want price + stock delta 6 + stock state", filtered)
	}
}

func TestNotifyReportsBatchesAndSendsAllChannels(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		counts[request.URL.Path]++
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/push" {
			_, _ = writer.Write([]byte(`{"code":200}`))
			return
		}
		_, _ = writer.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	notifier, err := New(Config{
		Enabled:   true,
		DingTalk:  DingTalkConfig{Enabled: Bool(true), WebhookURL: server.URL + "/dingtalk"},
		Bark:      BarkConfig{Enabled: true, ServerURL: server.URL, DeviceKey: "device"},
		Format:    FormatText,
		DeviceTag: "abcd1234",
	}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reports := make([]Report, 4)
	for index := range reports {
		reports[index] = Report{ItemID: string(rune('1' + index)), Name: "商品", FinalPriceCents: 100, Changes: []FieldChange{{Category: "price", Field: "到手价", Numeric: true, OldNumber: 200, NewNumber: 100}}}
	}
	if err := notifier.NotifyReports(reports); err != nil {
		t.Fatalf("NotifyReports() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["/dingtalk"] != 2 || counts["/push"] != 2 {
		t.Fatalf("channel counts = %+v, want two batches per channel", counts)
	}
}

func TestReportMessageIncludesDeviceTagAndTruncates(t *testing.T) {
	notifier, _ := New(Config{Format: FormatText, DeviceTag: "tail-code"}, nil)
	body := notifier.buildReportBatch([]Report{{
		ItemID: "1", Name: strings.Repeat("长", maxMessageRunes),
		Changes: []FieldChange{{Category: "price", Field: "显示价", Old: "1", New: "2"}},
	}})
	if !strings.Contains(body, "已截断") {
		t.Fatal("long report was not truncated")
	}
	short := notifier.buildReportBatch([]Report{{ItemID: "1", Name: "商品", Changes: []FieldChange{{Category: "price", Field: "显示价", Old: "1", New: "2"}}}})
	if !strings.Contains(short, "识别码：tail-code") {
		t.Fatalf("device tag missing: %s", short)
	}
}

func TestCustomTemplateKeepsCategorizedDetails(t *testing.T) {
	notifier, err := New(Config{Template: "摘要 {{.Name}}"}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := notifier.buildReport(Report{
		ItemID: "1", Name: "商品", FinalPriceCents: 100,
		Changes: []FieldChange{{Category: "stock", Field: "库存状态", Old: "有货", New: "无货"}},
	})
	if !strings.Contains(body, "摘要 商品") || !strings.Contains(body, "库存变化") || !strings.Contains(body, "有货 -> 无货") {
		t.Fatalf("custom report lost details: %s", body)
	}
}
