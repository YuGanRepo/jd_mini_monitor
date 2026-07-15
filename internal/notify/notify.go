// Package notify sends JD (京东) cart price-change notifications to a DingTalk
// (钉钉) group robot webhook. It supports a configurable discount rate and a
// user-editable message template rendered per changed SKU.
//
// Only the subset of the original browser-extension notification stack that the
// project actually needs is implemented here:
//   - DingTalk webhook delivery (with optional HMAC-SHA256 signing)
//   - a discount-rate price calculation
//   - a text/markdown message template
package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Format selects the DingTalk message body type.
const (
	FormatText     = "text"
	FormatMarkdown = "markdown"
)

// DefaultTextTemplate is used when Config.Template is empty and Format is text.
const DefaultTextTemplate = "【京东购物车价格变动】{{.Name}}\n" +
	"SKU: {{.ItemID}}\n" +
	"现价: ¥{{.FinalYuan}}（原价 ¥{{.PrevYuan}}，{{.DeltaYuan}}）\n" +
	"折后价: ¥{{.DiscountYuan}}\n" +
	"库存: {{.StockDesc}}"

// DefaultMarkdownTemplate is used when Config.Template is empty and Format is markdown.
const DefaultMarkdownTemplate = "**{{.Name}}**\n\n" +
	"- SKU: `{{.ItemID}}`\n" +
	"- 现价: ¥{{.FinalYuan}} （原价 ¥{{.PrevYuan}}，{{.DeltaYuan}}）\n" +
	"- 折后价: **¥{{.DiscountYuan}}**\n" +
	"- 库存: {{.StockDesc}}"

// DingTalkConfig holds the target group robot webhook and optional signing secret.
type DingTalkConfig struct {
	WebhookURL string `json:"webhookUrl"`
	Secret     string `json:"secret,omitempty"`
}

// Config is the complete notification + pricing configuration. It is safe to
// persist as JSON.
type Config struct {
	Enabled  bool           `json:"enabled"`
	DingTalk DingTalkConfig `json:"dingtalk"`
	// DiscountRate is a multiplier applied to the final price to obtain the
	// "折后价". For example 0.95 means 95折. Values <= 0 or >= 1 disable the
	// discount (the discounted price equals the final price).
	DiscountRate float64 `json:"discountRate"`
	// Format selects the DingTalk body type: "text" (default) or "markdown".
	Format string `json:"format"`
	// Title is the markdown card title (ignored for text messages).
	Title string `json:"title"`
	// Template is a Go text/template rendered once per changed SKU. When empty a
	// sensible default for the selected Format is used.
	Template string `json:"template"`
}

// Change is a single SKU whose final price changed on the latest capture.
type Change struct {
	ItemID     string
	Name       string
	VendorName string
	Num        int
	StockDesc  string
	FinalCents int64
	PrevCents  int64
	DeltaCents int64
}

// templateData is what the message template can reference.
type templateData struct {
	ItemID       string
	Name         string
	VendorName   string
	Num          int
	StockDesc    string
	FinalYuan    string
	PrevYuan     string
	DeltaYuan    string
	DiscountYuan string
	FinalCents   int64
	PrevCents    int64
	DeltaCents   int64
	Up           bool
	Down         bool
}

// Notifier renders and delivers notifications for a fixed configuration.
type Notifier struct {
	config Config
	tmpl   *template.Template
	client *http.Client
	logger *log.Logger
	now    func() time.Time
}

// New builds a Notifier from config. It returns an error if the template fails
// to parse. logger may be nil.
func New(config Config, logger *log.Logger) (*Notifier, error) {
	if logger == nil {
		logger = log.Default()
	}
	if config.Format == "" {
		config.Format = FormatText
	}
	tmplText := config.Template
	if strings.TrimSpace(tmplText) == "" {
		if config.Format == FormatMarkdown {
			tmplText = DefaultMarkdownTemplate
		} else {
			tmplText = DefaultTextTemplate
		}
	}
	tmpl, err := template.New("notify").Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("parse notify template: %w", err)
	}
	return &Notifier{
		config: config,
		tmpl:   tmpl,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
		now:    time.Now,
	}, nil
}

// Enabled reports whether notifications are configured to be sent.
func (n *Notifier) Enabled() bool {
	return n != nil && n.config.Enabled && strings.TrimSpace(n.config.DingTalk.WebhookURL) != ""
}

// discountCents applies the configured discount rate to a final price.
func (n *Notifier) discountCents(finalCents int64) int64 {
	rate := n.config.DiscountRate
	if rate <= 0 || rate >= 1 {
		return finalCents
	}
	// Round to the nearest cent.
	return int64(float64(finalCents)*rate + 0.5)
}

// render turns a single change into the template output string.
func (n *Notifier) render(change Change) (string, error) {
	data := templateData{
		ItemID:       change.ItemID,
		Name:         change.Name,
		VendorName:   change.VendorName,
		Num:          change.Num,
		StockDesc:    change.StockDesc,
		FinalYuan:    formatYuan(change.FinalCents),
		PrevYuan:     formatYuan(change.PrevCents),
		DeltaYuan:    formatDeltaYuan(change.DeltaCents),
		DiscountYuan: formatYuan(n.discountCents(change.FinalCents)),
		FinalCents:   change.FinalCents,
		PrevCents:    change.PrevCents,
		DeltaCents:   change.DeltaCents,
		Up:           change.DeltaCents > 0,
		Down:         change.DeltaCents < 0,
	}
	var buffer bytes.Buffer
	if err := n.tmpl.Execute(&buffer, data); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// BuildMessage renders all changes into the DingTalk payload body text. The
// returned string joins per-SKU renders; it is exported so callers can preview a
// message (e.g. a "test" button) without sending it.
func (n *Notifier) BuildMessage(changes []Change) (string, error) {
	if len(changes) == 0 {
		return "", errors.New("no changes to render")
	}
	separator := "\n"
	if n.config.Format == FormatMarkdown {
		separator = "\n\n---\n\n"
	}
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		rendered, err := n.render(change)
		if err != nil {
			return "", err
		}
		parts = append(parts, rendered)
	}
	return strings.Join(parts, separator), nil
}

// Notify renders the changes and posts them to the DingTalk webhook. It is a
// no-op (returns nil) when notifications are disabled or there are no changes.
func (n *Notifier) Notify(changes []Change) error {
	if !n.Enabled() || len(changes) == 0 {
		return nil
	}
	body, err := n.BuildMessage(changes)
	if err != nil {
		return err
	}
	return n.post(body)
}

// SendTest posts a fixed sample message so users can verify webhook + signing.
func (n *Notifier) SendTest() error {
	if strings.TrimSpace(n.config.DingTalk.WebhookURL) == "" {
		return errors.New("dingtalk webhook url is empty")
	}
	sample := Change{
		ItemID:     "100012043978",
		Name:       "测试商品（Mini Proxy 通知自检）",
		Num:        1,
		StockDesc:  "有货",
		FinalCents: 9900,
		PrevCents:  12900,
		DeltaCents: -3000,
	}
	body, err := n.BuildMessage([]Change{sample})
	if err != nil {
		return err
	}
	return n.post(body)
}

// post builds the DingTalk payload for the configured format, signs the URL when
// a secret is set, and sends the HTTP request.
func (n *Notifier) post(body string) error {
	var payload any
	switch n.config.Format {
	case FormatMarkdown:
		title := n.config.Title
		if strings.TrimSpace(title) == "" {
			title = "京东购物车价格变动"
		}
		payload = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  body,
			},
		}
	default:
		payload = map[string]any{
			"msgtype": "text",
			"text": map[string]string{
				"content": body,
			},
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint, err := n.signedURL()
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("dingtalk webhook returned status %d", response.StatusCode)
	}

	// DingTalk returns errcode != 0 in the body for logical failures (e.g. bad
	// keyword / signature) even with HTTP 200.
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err == nil && result.ErrCode != 0 {
		return fmt.Errorf("dingtalk webhook error %d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

// signedURL appends the timestamp/sign query parameters required by DingTalk
// robots that enable the "加签" (signing) security setting. When no secret is
// configured the raw webhook URL is returned unchanged.
func (n *Notifier) signedURL() (string, error) {
	secret := strings.TrimSpace(n.config.DingTalk.Secret)
	if secret == "" {
		return n.config.DingTalk.WebhookURL, nil
	}
	timestamp := strconv.FormatInt(n.now().UnixMilli(), 10)
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", err
	}
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	parsed, err := url.Parse(n.config.DingTalk.WebhookURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("timestamp", timestamp)
	query.Set("sign", sign)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// formatYuan renders integer cents as a fixed 2-decimal yuan string.
func formatYuan(cents int64) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	yuan := cents / 100
	frac := cents % 100
	sign := ""
	if negative {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, yuan, frac)
}

// formatDeltaYuan renders a signed delta with a leading +/- and a Chinese hint.
func formatDeltaYuan(cents int64) string {
	switch {
	case cents > 0:
		return "涨 +" + formatYuan(cents)
	case cents < 0:
		return "降 -" + formatYuan(-cents)
	default:
		return "持平 0.00"
	}
}
