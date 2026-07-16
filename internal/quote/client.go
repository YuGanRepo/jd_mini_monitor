package quote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 10 * time.Minute

type Match struct {
	Name        string  `json:"name"`
	SinglePrice float64 `json:"singlePrice"`
	CasePrice   float64 `json:"casePrice"`
	CaseDivisor int     `json:"caseDivisor"`
	CasePerUnit float64 `json:"casePerUnit"`
}

type Diff struct {
	Amount       float64
	PricePerUnit float64
	QuoteTotal   float64
	CostTotal    float64
	Spec         string
	QuoteName    string
	ProfitRatio  float64
}

func CalculateDiff(name string, finalPriceCents int64, discountRate float64, match *Match) *Diff {
	if match == nil || finalPriceCents <= 0 {
		return nil
	}
	if discountRate <= 0 || discountRate >= 1 {
		discountRate = 1
	}
	perUnit, spec := pickPerUnit(match, name)
	if perUnit <= 0 {
		return nil
	}
	divisor := PackageDivisor(name)
	costPerUnit := float64(finalPriceCents) / 100 * discountRate / float64(divisor)
	totalCost := costPerUnit * float64(divisor)
	quoteTotal := perUnit * float64(divisor)
	difference := quoteTotal - totalCost
	profitRatio := 0.0
	if totalCost > 0 {
		profitRatio = difference / totalCost
	}
	return &Diff{
		Amount: difference, PricePerUnit: perUnit, QuoteTotal: quoteTotal, CostTotal: totalCost,
		Spec: spec, QuoteName: match.Name, ProfitRatio: profitRatio,
	}
}

func pickPerUnit(match *Match, name string) (float64, string) {
	if packageLevel(name) == "case" && match.CasePerUnit > 0 {
		return match.CasePerUnit, "原箱"
	}
	if match.SinglePrice > 0 {
		return match.SinglePrice, "单瓶"
	}
	if match.CasePerUnit > 0 {
		return match.CasePerUnit, "原箱"
	}
	return 0, ""
}

func PackageDivisor(name string) int {
	if match := regexp.MustCompile(`(?i)(?:\*|x|×)\s*(\d{1,2})`).FindStringSubmatch(name); len(match) == 2 {
		if count, err := strconv.Atoi(match[1]); err == nil && count > 1 {
			return count
		}
	}
	if match := regexp.MustCompile(`(\d{1,2})\s*(?:瓶|支)`).FindStringSubmatch(name); len(match) == 2 {
		if count, err := strconv.Atoi(match[1]); err == nil && count > 1 {
			return count
		}
	}
	if strings.Contains(name, "整箱") || strings.Contains(name, "原箱") || strings.Contains(name, "原件") {
		return 6
	}
	return 1
}

func packageLevel(name string) string {
	for _, keyword := range []string{"整箱", "原箱", "原件", "整件", "一提", "提装"} {
		if strings.Contains(name, keyword) {
			return "case"
		}
	}
	for _, keyword := range []string{"单瓶", "散瓶", "散装", "单支", "单只", "裸瓶", "双瓶", "两瓶", "双支", "两支"} {
		if strings.Contains(name, keyword) {
			return "single"
		}
	}
	return "unknown"
}

type cacheEntry struct {
	at    time.Time
	value *Match
}

type Client struct {
	baseURL  string
	key      string
	deviceID string
	http     *http.Client
	mu       sync.Mutex
	cache    map[string]cacheEntry
	now      func() time.Time
}

func NewClient(baseURL, key, deviceID string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		key:      strings.TrimSpace(key),
		deviceID: strings.TrimSpace(deviceID),
		http:     &http.Client{Timeout: 12 * time.Second},
		cache:    make(map[string]cacheEntry),
		now:      time.Now,
	}
}

func (client *Client) Match(skuID, name string) (*Match, error) {
	skuID = strings.TrimSpace(skuID)
	name = strings.TrimSpace(name)
	if skuID == "" || name == "" || client.key == "" || client.deviceID == "" {
		return nil, nil
	}
	cacheKey := skuID + "\n" + name
	client.mu.Lock()
	cached, ok := client.cache[cacheKey]
	if ok && client.now().Sub(cached.at) < cacheTTL {
		client.mu.Unlock()
		return cached.value, nil
	}
	client.mu.Unlock()

	raw, err := json.Marshal(map[string]string{"sku": skuID, "name": name, "key": client.key, "deviceId": client.deviceID})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, client.baseURL+"/api/quote/match", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Match *Match `json:"match"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("quote server returned invalid response (HTTP %d)", response.StatusCode)
	}
	if !result.OK {
		return nil, fmt.Errorf("quote match failed: %s", result.Error)
	}
	client.mu.Lock()
	client.cache[cacheKey] = cacheEntry{at: client.now(), value: result.Match}
	client.mu.Unlock()
	return result.Match, nil
}
