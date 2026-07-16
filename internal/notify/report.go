package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxReportsPerMessage = 3
	maxMessageRunes      = 18000
)

type FieldChange struct {
	Category    string
	Field       string
	Old         string
	New         string
	Description string
	OldNumber   int64
	NewNumber   int64
	Numeric     bool
}

type Report struct {
	ItemID            string
	Name              string
	VendorName        string
	Num               int
	StockDesc         string
	RemainNum         int
	PagePriceCents    int64
	FinalPriceCents   int64
	ProductURL        string
	CheckoutURL       string
	Changes           []FieldChange
	HasQuote          bool
	QuoteName         string
	QuoteSpec         string
	QuotePricePerUnit float64
	QuoteDiff         float64
	ProfitRatio       float64
}

func (n *Notifier) NotifyReports(reports []Report) error {
	if !n.Enabled() || len(reports) == 0 {
		return nil
	}
	filtered := make([]Report, 0, len(reports))
	for _, report := range reports {
		report.Changes = n.filterChanges(report.Changes)
		if len(report.Changes) > 0 {
			filtered = append(filtered, report)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	var failures []string
	for start := 0; start < len(filtered); start += maxReportsPerMessage {
		end := start + maxReportsPerMessage
		if end > len(filtered) {
			end = len(filtered)
		}
		batch := filtered[start:end]
		body := n.buildReportBatch(batch)
		title := n.config.Title
		if strings.TrimSpace(title) == "" {
			title = fmt.Sprintf("购物车变更通知（共 %d 商品）", len(batch))
		}
		if err := n.sendConfigured(body, title); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (n *Notifier) filterChanges(changes []FieldChange) []FieldChange {
	categories := n.config.Categories
	enabled := func(category string) bool {
		if categories == nil {
			return true
		}
		switch category {
		case "price":
			return categories.Price
		case "stock":
			return categories.Stock
		case "promo":
			return categories.Promo
		case "gift":
			return categories.Gift
		default:
			return true
		}
	}
	filtered := make([]FieldChange, 0, len(changes))
	for _, change := range changes {
		if !enabled(change.Category) {
			continue
		}
		if change.Category == "stock" && change.Field == "剩余库存" && change.Numeric {
			delta := change.NewNumber - change.OldNumber
			if delta < 0 {
				delta = -delta
			}
			if delta <= int64(n.config.StockChangeThreshold) {
				continue
			}
		}
		filtered = append(filtered, change)
	}
	return filtered
}

func (n *Notifier) buildReportBatch(reports []Report) string {
	sections := make([]string, 0, len(reports))
	for _, report := range reports {
		sections = append(sections, n.buildReport(report))
	}
	separator := "\n\n"
	if n.config.Format == FormatMarkdown {
		separator = "\n\n---\n\n"
	}
	body := strings.Join(sections, separator)
	if n.config.DeviceTag != "" {
		if n.config.Format == FormatMarkdown {
			body += "\n\n---\n识别码：`" + n.config.DeviceTag + "`"
		} else {
			body += "\n识别码：" + n.config.DeviceTag
		}
	}
	return truncateRunes(body, maxMessageRunes)
}

func (n *Notifier) buildReport(report Report) string {
	customHeader := ""
	if strings.TrimSpace(n.config.Template) != "" {
		previous := report.FinalPriceCents
		for _, change := range report.Changes {
			if change.Category == "price" && change.Field == "到手价" && change.Numeric {
				previous = change.OldNumber
				break
			}
		}
		custom, err := n.render(Change{
			ItemID: report.ItemID, Name: report.Name, VendorName: report.VendorName, Num: report.Num,
			StockDesc: report.StockDesc, FinalCents: report.FinalPriceCents, PrevCents: previous,
			DeltaCents: report.FinalPriceCents - previous,
		})
		if err == nil {
			customHeader = custom
		}
	}
	grouped := map[string][]FieldChange{}
	for _, change := range report.Changes {
		grouped[change.Category] = append(grouped[change.Category], change)
	}
	landed := formatYuan(report.FinalPriceCents)
	stock := report.StockDesc
	if report.RemainNum >= 0 {
		stock = fmt.Sprintf("%s（剩余 %d 件）", firstText(stock, "未知"), report.RemainNum)
	}
	lines := []string{fmt.Sprintf("%s - ¥%s", firstText(report.Name, report.ItemID), landed)}
	if customHeader != "" {
		lines[0] = customHeader
	}
	labels := []struct{ key, label string }{{"price", "价格"}, {"stock", "库存"}, {"promo", "优惠"}, {"gift", "赠品"}}
	for _, category := range labels {
		changes := grouped[category.key]
		if len(changes) == 0 {
			continue
		}
		if n.config.Format == FormatMarkdown {
			lines = append(lines, "**"+category.label+"变化**")
		} else {
			lines = append(lines, category.label+"变化")
		}
		for _, change := range changes {
			description := ""
			if change.Description != "" {
				description = "（" + change.Description + "）"
			}
			lines = append(lines, fmt.Sprintf("- %s：%s -> %s%s", change.Field, displayChangeValue(change, true), displayChangeValue(change, false), description))
		}
	}
	lines = append(lines, fmt.Sprintf("页面价：¥%s   到手价：¥%s   库存：%s", formatYuan(report.PagePriceCents), landed, firstText(stock, "未知")))
	if discounted := n.discountedPrice(report); discounted != "" {
		lines = append(lines, discounted)
	}
	if report.HasQuote {
		diffSign := ""
		if report.QuoteDiff >= 0 {
			diffSign = "+"
		}
		quoteLine := fmt.Sprintf("报价对比：%s %s = ¥%.2f   差价：%s%.2f", firstText(report.QuoteName, "服务端报价"), report.QuoteSpec, report.QuotePricePerUnit, diffSign, report.QuoteDiff)
		if report.QuoteDiff > 0 {
			quoteLine += fmt.Sprintf("（%.1f%%）", report.ProfitRatio*100)
		}
		lines = append(lines, quoteLine)
	}
	if n.config.ShowProductURL && report.ProductURL != "" {
		lines = append(lines, "商品链接："+report.ProductURL)
	}
	if n.config.ShowCheckoutURL && report.CheckoutURL != "" {
		lines = append(lines, "支付链接："+report.CheckoutURL)
	}
	if n.config.ShowAppQRCode && report.ItemID != "" {
		lines = append(lines, "APP&扫码：https://www.axureshow.com/project/uaSlvkaG/?skuId="+report.ItemID)
	}
	return strings.Join(lines, "\n")
}

func (n *Notifier) discountedPrice(report Report) string {
	rate := n.config.DiscountRate
	if rate <= 0 {
		return ""
	}
	divisor := packageDivisor(report.Name)
	price := float64(report.FinalPriceCents) * rate / float64(divisor) / 100
	return fmt.Sprintf("预估折后价：¥%.2f（%.3g 折%s）", price, rate, divisorText(divisor))
}

func packageDivisor(name string) int {
	explicit := regexp.MustCompile(`(?i)(?:\*|x|×)\s*(\d{1,2})`).FindStringSubmatch(name)
	if len(explicit) == 2 {
		if count, err := strconv.Atoi(explicit[1]); err == nil && count > 1 {
			return count
		}
	}
	bottles := regexp.MustCompile(`(\d{1,2})\s*(?:瓶|支)`).FindStringSubmatch(name)
	if len(bottles) == 2 {
		if count, err := strconv.Atoi(bottles[1]); err == nil && count > 1 {
			return count
		}
	}
	if strings.Contains(name, "整箱") || strings.Contains(name, "原箱") || strings.Contains(name, "原件") {
		return 6
	}
	return 1
}

func divisorText(divisor int) string {
	if divisor > 1 {
		return fmt.Sprintf(" / %d件分摊", divisor)
	}
	return ""
}

func displayChangeValue(change FieldChange, old bool) string {
	value := change.New
	number := change.NewNumber
	if old {
		value = change.Old
		number = change.OldNumber
	}
	if !change.Numeric {
		return firstText(value, "(无)")
	}
	if change.Field == "剩余库存" {
		return strconv.FormatInt(number, 10)
	}
	return "¥" + formatYuan(number)
}

func (n *Notifier) sendConfigured(body, title string) error {
	type result struct {
		channel string
		err     error
	}
	results := make(chan result, 2)
	count := 0
	if n.dingTalkEnabled() {
		count++
		go func() { results <- result{channel: "dingtalk", err: n.postDingTalk(body, title)} }()
	}
	if n.barkEnabled() {
		count++
		go func() { results <- result{channel: "bark", err: n.postBark(body, title)} }()
	}
	if count == 0 {
		return errors.New("no notification channel is enabled")
	}
	var failures []string
	for index := 0; index < count; index++ {
		result := <-results
		if result.err != nil {
			failures = append(failures, result.channel+": "+result.err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (n *Notifier) postDingTalk(body, title string) error {
	payload := any(map[string]any{"msgtype": "text", "text": map[string]string{"content": title + "\n" + body}})
	if n.config.Format == FormatMarkdown {
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": title, "text": "### " + title + "\n\n" + body}}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint, err := n.signedURL()
	if err != nil {
		return err
	}
	return n.postJSON(endpoint, raw, true)
}

func (n *Notifier) postBark(body, title string) error {
	endpoint := strings.TrimRight(strings.TrimSpace(n.config.Bark.ServerURL), "/") + "/push"
	raw, err := json.Marshal(map[string]string{
		"device_key": n.config.Bark.DeviceKey,
		"title":      title,
		"body":       truncateRunes(body, maxMessageRunes),
		"group":      "jd-cart-recorder",
	})
	if err != nil {
		return err
	}
	return n.postJSON(endpoint, raw, false)
}

func (n *Notifier) postJSON(endpoint string, raw []byte, dingTalk bool) error {
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
		return fmt.Errorf("webhook returned status %d", response.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return nil
	}
	if dingTalk {
		if code, ok := data["errcode"].(float64); ok && code != 0 {
			return fmt.Errorf("dingtalk webhook error %.0f: %v", code, data["errmsg"])
		}
	} else if code, ok := data["code"].(float64); ok && code != 200 {
		return fmt.Errorf("bark webhook error %.0f: %v", code, data["message"])
	}
	return nil
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "\n...(已截断)"
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
