// Package sku parses JD (京东) cartview responses into a normalized SKU list and
// keeps a running store that is updated on every interception, tracking price
// changes between captures.
package sku

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SKU is one normalized cart line extracted from a cartview response.
type SKU struct {
	ItemID           string  `json:"itemId"`
	Name             string  `json:"name"`
	VendorID         string  `json:"vendorId"`
	VendorName       string  `json:"vendorName"`
	PagePriceCents   int64   `json:"pagePriceCents"`
	FinalPriceCents  int64   `json:"finalPriceCents"`
	DiscountCents    int64   `json:"discountCents"`
	Num              int     `json:"num"`
	StockCode        int     `json:"stockCode"`
	StockDesc        string  `json:"stockDesc"`
	RemainNum        int     `json:"remainNum"`
	PricePrim        string  `json:"pricePrim,omitempty"`
	PriceShow        string  `json:"priceShow,omitempty"`
	PriceDescription string  `json:"priceDescription,omitempty"`
	PriceRevert      string  `json:"priceRevert,omitempty"`
	PlusDiscount     string  `json:"plusDiscount,omitempty"`
	PlusText         string  `json:"plusText,omitempty"`
	SelectedPromos   []Promo `json:"selectedPromos,omitempty"`
	CutPriceText     string  `json:"cutPriceText,omitempty"`
	CutPriceCents    int64   `json:"cutPriceCents,omitempty"`
	Gifts            []Gift  `json:"gifts,omitempty"`
	SubsidyText      string  `json:"subsidyText,omitempty"`
	ProductURL       string  `json:"productUrl,omitempty"`
	CheckoutURL      string  `json:"checkoutUrl,omitempty"`
}

type Promo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type,omitempty"`
}

type Gift struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Num  int    `json:"num"`
}

// flatSku mirrors a single entry in cartInfo.flatSkus (prices are decimal yuan
// strings like "13239.00", not integer cents).
type flatSku struct {
	ItemID     flexibleString `json:"itemId"`
	ItemName   string         `json:"itemName"`
	Price      flexibleString `json:"price"`
	Num        int            `json:"itemNum"`
	StockCode  int            `json:"stockStatus"`
	StockDesc  string         `json:"-"`
	VendorID   flexibleString `json:"vendorId"`
	IsNoCheck  bool           `json:"isNoCheck"`
	ItemStatus string         `json:"itemStatus"`
}

// flexibleString accepts JSON strings and numbers. JD cartview uses both forms
// for ids/prices depending on where the field appears (e.g. flatSkus.vendorId is
// sometimes a number while vendorList.baseInfo.vendorId is a string).
type flexibleString string

func (value *flexibleString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*value = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = flexibleString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*value = flexibleString(number.String())
		return nil
	}
	return nil
}

type cartItem struct {
	ItemID      string `json:"itemId"`
	ItemName    string `json:"itemName"`
	Price       string `json:"price"`
	LandedPrice string `json:"landedPrice"`
	Num         int    `json:"num"`
	StockCode   int    `json:"stockCode"`
	StockDesc   string `json:"stockDesc"`
}

type cartVendor struct {
	BaseInfo struct {
		VendorID   string `json:"vendorId"`
		VendorName string `json:"vendorName"`
	} `json:"baseInfo"`
	Items []cartItem `json:"items"`
}

type cartInfo struct {
	VendorList []cartVendor       `json:"vendorList"`
	FlatSkus   map[string]flatSku `json:"flatSkus"`
}

type cartviewPayload struct {
	CartInfo cartInfo `json:"cartInfo"`
	Data     *struct {
		CartInfo cartInfo `json:"cartInfo"`
	} `json:"data"`
	ResultData *pluginResultData `json:"resultData"`
}

type pluginResultData struct {
	CartInfo struct {
		Vendors []pluginVendor `json:"vendors"`
	} `json:"cartInfo"`
}

type pluginVendor struct {
	ShopName   string         `json:"shopName"`
	VendorName string         `json:"vendorName"`
	VendorID   flexibleString `json:"vendorId"`
	Sorted     []pluginEntry  `json:"sorted"`
}

type pluginEntry struct {
	ItemType int            `json:"itemType"`
	Item     map[string]any `json:"item"`
}

// ParseCartview parses a raw cartview JSON body into a flat SKU list.
//
// Strategy (order of preference):
//  1. cartInfo.flatSkus — always present and covers every cart line (not paginated).
//     Prices here are decimal-yuan strings like "13239.00". Matching vendorList
//     entries enrich these rows with landedPrice, which is the actual final price.
//  2. cartInfo.vendorList[*].items — fallback (paginated, may be incomplete).
//     Prices here are integer-cent strings like "1323900".
//
// The "data" wrapper ({"data": {"cartInfo": ...}}) is accepted but not required.
func ParseCartview(data []byte) ([]SKU, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, errors.New("empty cartview body")
	}

	var payload cartviewPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload.ResultData != nil && len(payload.ResultData.CartInfo.Vendors) > 0 {
		return fromPluginVendors(payload.ResultData.CartInfo.Vendors), nil
	}

	info := payload.CartInfo
	if len(info.FlatSkus) == 0 && len(info.VendorList) == 0 && payload.Data != nil {
		info = payload.Data.CartInfo
	}

	// 1) Prefer flatSkus (full set, never paginated).
	if len(info.FlatSkus) > 0 {
		return fromFlatSkus(info)
	}

	// 2) Fall back to vendorList.
	return fromVendorList(info)
}

func fromPluginVendors(vendors []pluginVendor) []SKU {
	out := make([]SKU, 0)
	for _, vendor := range vendors {
		for _, entry := range vendor.Sorted {
			if entry.ItemType != 1 || entry.Item == nil {
				continue
			}
			item := entry.Item
			itemID := mapString(item, "skuId", "Id", "id")
			if itemID == "" {
				continue
			}
			stock := mapObject(item["stock"])
			page := decimalYuanValueToCents(firstMapValue(item, "Price"))
			final := decimalYuanValueToCents(firstMapValue(item, "landedPrice"))
			if final <= 0 {
				final = decimalYuanValueToCents(firstMapValue(item, "PriceShow", "priceShow", "priceJd"))
			}
			if final <= 0 {
				final = page
			}
			discount := decimalYuanValueToCents(firstMapValue(item, "Discount"))
			if discount == 0 && page > final {
				discount = page - final
			}
			stockDesc := firstNonEmpty(mapString(stock, "stockState"), mapString(item, "stockState"))
			stockCode := mapInt(stock, "stockStateId")
			if stockCode == 0 {
				stockCode = mapInt(item, "stockStateId")
			}
			remainNum := mapIntDefault(stock, -1, "remainNumInt", "remainNum")
			if remainNum == -1 {
				remainNum = mapIntDefault(item, -1, "remainNumInt", "remainNum")
			}
			name := mapString(item, "skuName", "Name", "name", "title", "wareName")
			num := mapIntDefault(item, 1, "num", "Num", "count", "Count", "quantity")
			out = append(out, SKU{
				ItemID: itemID, Name: name, VendorID: string(vendor.VendorID), VendorName: firstNonEmpty(vendor.ShopName, vendor.VendorName),
				PagePriceCents: page, FinalPriceCents: final, DiscountCents: discount, Num: num,
				StockCode: stockCode, StockDesc: stockDesc, RemainNum: remainNum,
				PricePrim: mapString(item, "pricePrim"), PriceShow: mapString(item, "PriceShow", "priceShow", "priceJd"),
				PriceDescription: mapString(item, "priceDes"), PriceRevert: mapString(item, "priceRevert"),
				PlusDiscount:   nestedString(item, "cardPromotionFloor", "discount", "discountPrice"),
				PlusText:       nestedString(item, "cardPromotionFloor", "showText", "text"),
				SelectedPromos: collectPromos(item), CutPriceText: mapString(item, "cutPriceT"),
				CutPriceCents: decimalYuanValueToCents(item["cut"]), Gifts: collectGifts(item),
				SubsidyText: collectSubsidyText(item), ProductURL: productURL(itemID), CheckoutURL: checkoutURL(itemID, num),
			})
		}
	}
	return out
}

func firstMapValue(record map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := record[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return value
		}
	}
	return nil
}

func mapString(record map[string]any, keys ...string) string {
	value := firstMapValue(record, keys...)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mapObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	return object
}

func mapInt(record map[string]any, keys ...string) int {
	return mapIntDefault(record, 0, keys...)
}

func mapIntDefault(record map[string]any, fallback int, keys ...string) int {
	value := firstMapValue(record, keys...)
	if value == nil {
		return fallback
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	if err != nil {
		return fallback
	}
	return int(number)
}

func decimalYuanValueToCents(value any) int64 {
	if value == nil {
		return 0
	}
	return parseDecimalCents(strings.TrimSpace(fmt.Sprint(value)))
}

func nestedString(record map[string]any, objectKey string, keys ...string) string {
	return mapString(mapObject(record[objectKey]), keys...)
}

func collectPromos(item map[string]any) []Promo {
	values, _ := item["selectedPromoList"].([]any)
	out := make([]Promo, 0, len(values))
	for _, value := range values {
		promo := mapObject(value)
		out = append(out, Promo{ID: mapString(promo, "id", "pid", "promoId"), Title: mapString(promo, "title"), Type: mapString(promo, "type")})
	}
	return out
}

func collectGifts(item map[string]any) []Gift {
	groups, _ := item["giftGroupInfosShow"].([]any)
	var out []Gift
	for _, value := range groups {
		infos, _ := mapObject(value)["giftInfos"].([]any)
		for _, infoValue := range infos {
			gift := mapObject(infoValue)
			out = append(out, Gift{ID: mapString(gift, "id", "skuId", "giftSkuId", "name"), Name: mapString(gift, "name"), Num: mapIntDefault(gift, 1, "giftNum")})
		}
	}
	return out
}

func collectSubsidyText(item map[string]any) string {
	priceBottom, _ := mapObject(item["skuLabels"])["priceBottom"].([]any)
	for _, value := range priceBottom {
		if text := mapString(mapObject(value), "t", "vt"); text != "" {
			return text
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func productURL(itemID string) string {
	return "https://item.jd.com/" + url.PathEscape(itemID) + ".html"
}

func checkoutURL(itemID string, count int) string {
	if count < 1 {
		count = 1
	}
	return fmt.Sprintf("https://trade.m.jd.com/pay?commlist=%s,,%d#/index", url.QueryEscape(itemID), count)
}

// fromFlatSkus builds the SKU list from cartInfo.flatSkus (prices are decimal yuan).
func fromFlatSkus(info cartInfo) ([]SKU, error) {
	// Build a vendorID→name lookup from vendorList to decorate flat entries.
	vendorNameByID := map[string]string{}
	vendorItemByID := map[string]cartItem{}
	for _, v := range info.VendorList {
		if vid := v.BaseInfo.VendorID; vid != "" {
			vendorNameByID[vid] = v.BaseInfo.VendorName
		}
		for _, item := range v.Items {
			if strings.TrimSpace(item.ItemID) != "" {
				vendorItemByID[item.ItemID] = item
			}
		}
	}

	out := make([]SKU, 0, len(info.FlatSkus))
	for _, item := range info.FlatSkus {
		itemID := string(item.ItemID)
		vendorID := string(item.VendorID)
		if strings.TrimSpace(itemID) == "" {
			continue
		}
		// flatSkus.price is the page price in decimal yuan. vendorList uses
		// integer cents and carries landedPrice, the price JD labels 到手价.
		page := parseDecimalCents(string(item.Price))
		final := page
		if vendorItem, ok := vendorItemByID[itemID]; ok {
			if vendorPage := parseIntegerCents(vendorItem.Price); vendorPage > 0 {
				page = vendorPage
			}
			if landedPrice := parseIntegerCents(vendorItem.LandedPrice); landedPrice > 0 {
				final = landedPrice
			} else {
				final = page
			}
		}
		discount := page - final
		if discount < 0 {
			discount = 0
		}
		stockCode := item.StockCode
		stockDesc := item.StockDesc
		if stockCode == 0 && item.IsNoCheck {
			// isNoCheck without stockCode: treat as unavailable (same as vendorList convention).
			// Keep stockCode 0 but add desc.
		}
		if stockCode == 1 && stockDesc == "" {
			stockDesc = "无货"
		}
		vendorName := vendorNameByID[vendorID]

		out = append(out, SKU{
			ItemID:          itemID,
			Name:            item.ItemName,
			VendorID:        vendorID,
			VendorName:      vendorName,
			PagePriceCents:  page,
			FinalPriceCents: final,
			DiscountCents:   discount,
			Num:             item.Num,
			StockCode:       stockCode,
			StockDesc:       stockDesc,
			RemainNum:       -1,
			ProductURL:      productURL(itemID),
			CheckoutURL:     checkoutURL(itemID, item.Num),
		})
	}
	return out, nil
}

// fromVendorList builds the SKU list from cartInfo.vendorList[*].items (prices
// are integer cents).
func fromVendorList(info cartInfo) ([]SKU, error) {
	out := make([]SKU, 0)
	for _, vendor := range info.VendorList {
		for _, item := range vendor.Items {
			if strings.TrimSpace(item.ItemID) == "" {
				continue
			}
			page := parseIntegerCents(item.Price)
			final := parseIntegerCents(item.LandedPrice)
			if final <= 0 {
				final = page
			}
			discount := page - final
			if discount < 0 {
				discount = 0
			}
			out = append(out, SKU{
				ItemID:          item.ItemID,
				Name:            item.ItemName,
				VendorID:        vendor.BaseInfo.VendorID,
				VendorName:      vendor.BaseInfo.VendorName,
				PagePriceCents:  page,
				FinalPriceCents: final,
				DiscountCents:   discount,
				Num:             item.Num,
				StockCode:       item.StockCode,
				StockDesc:       item.StockDesc,
				RemainNum:       -1,
				ProductURL:      productURL(item.ItemID),
				CheckoutURL:     checkoutURL(item.ItemID, item.Num),
			})
		}
	}
	return out, nil
}

// parseDecimalCents converts a decimal-yuan string (e.g. "13239.00") to integer
// cents. Returns 0 for empty/malformed strings.
func parseDecimalCents(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	// Remove any trailing ".00" then parse as integer; if a dot is present,
	// treat it as a float and multiply.
	if idx := strings.IndexByte(value, '.'); idx >= 0 {
		intPart := value[:idx]
		fracPart := value[idx+1:]
		if len(fracPart) > 2 {
			fracPart = fracPart[:2]
		}
		for len(fracPart) < 2 {
			fracPart += "0"
		}
		cents, err := strconv.ParseInt(intPart+fracPart, 10, 64)
		if err != nil {
			return 0
		}
		return cents
	}
	// flatSkus prices are yuan; if the decimal point is absent, treat it as
	// whole yuan.
	yuan, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return yuan * 100
}

// parseIntegerCents parses an integer-cent string (e.g. "1323900").
func parseIntegerCents(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	cents, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return cents
}

// Entry is a stored SKU plus the change-tracking metadata accumulated across
// interceptions.
type Entry struct {
	SKU
	FirstSeen       time.Time `json:"firstSeen"`
	LastUpdated     time.Time `json:"lastUpdated"`
	UpdateCount     int       `json:"updateCount"`
	PrevFinalCents  int64     `json:"prevFinalCents"`  // final price before the most recent change
	FinalDeltaCents int64     `json:"finalDeltaCents"` // FinalPriceCents - PrevFinalCents at last change (persists)
	PriceChanged    bool      `json:"priceChanged"`    // whether the final price changed on the latest capture
	StockChanged    bool      `json:"stockChanged"`
	PromoChanged    bool      `json:"promoChanged"`
	GiftChanged     bool      `json:"giftChanged"`
	Changes         []Change  `json:"changes,omitempty"`
	QuoteStatus     string    `json:"quoteStatus,omitempty"`
	QuoteName       string    `json:"quoteName,omitempty"`
	QuoteSpec       string    `json:"quoteSpec,omitempty"`
	QuotePrice      float64   `json:"quotePrice,omitempty"`
	QuoteTotal      float64   `json:"quoteTotal,omitempty"`
	QuoteCost       float64   `json:"quoteCost,omitempty"`
	QuoteDiff       float64   `json:"quoteDiff,omitempty"`
	QuoteProfitRate float64   `json:"quoteProfitRate,omitempty"`
	QuoteError      string    `json:"quoteError,omitempty"`
	QuoteUpdatedAt  time.Time `json:"quoteUpdatedAt,omitempty"`
}

const (
	QuoteStatusLoading   = "loading"
	QuoteStatusMatched   = "matched"
	QuoteStatusUnmatched = "unmatched"
	QuoteStatusError     = "error"
)

type QuoteResult struct {
	Status     string
	Name       string
	Spec       string
	Price      float64
	Total      float64
	Cost       float64
	Diff       float64
	ProfitRate float64
	Error      string
}

const (
	CategoryPrice = "price"
	CategoryStock = "stock"
	CategoryPromo = "promo"
	CategoryGift  = "gift"
)

type Change struct {
	Category    string `json:"category"`
	Field       string `json:"field"`
	Old         string `json:"old"`
	New         string `json:"new"`
	Description string `json:"description,omitempty"`
	OldNumber   int64  `json:"oldNumber,omitempty"`
	NewNumber   int64  `json:"newNumber,omitempty"`
	Numeric     bool   `json:"numeric,omitempty"`
}

// UpdateResult summarizes what a single Update call did.
type UpdateResult struct {
	Parsed  int `json:"parsed"`
	Changed int `json:"changed"`
	Total   int `json:"total"`
	// ChangedEntries holds a copy of every entry whose final price changed on
	// this capture, in no particular order. It is used to drive notifications.
	ChangedEntries []Entry `json:"changedEntries,omitempty"`
}

// Snapshot is an immutable copy of the store returned to callers/UI.
type Snapshot struct {
	Entries    []Entry   `json:"entries"`
	UpdatedAt  time.Time `json:"updatedAt"`
	ParseCount int       `json:"parseCount"`
	TotalSKU   int       `json:"totalSku"`
}

// Store keeps the latest known state for every SKU seen so far, keyed by item id,
// and is safe for concurrent use.
type Store struct {
	mu         sync.Mutex
	entries    map[string]*Entry
	parseCount int
	updatedAt  time.Time
}

// NewStore returns an empty store ready for use.
func NewStore() *Store {
	return &Store{entries: make(map[string]*Entry)}
}

// Update replaces the current cart snapshot, preserving metadata for items that
// remain and reporting categorized changes for previously known items.
func (store *Store) Update(skus []SKU) UpdateResult {
	store.mu.Lock()
	defer store.mu.Unlock()

	now := time.Now()
	store.parseCount++
	store.updatedAt = now
	nextEntries := make(map[string]*Entry, len(skus))
	changed := 0
	var changedEntries []Entry

	for _, item := range skus {
		existing, ok := store.entries[item.ItemID]
		if !ok {
			nextEntries[item.ItemID] = &Entry{
				SKU:            item,
				FirstSeen:      now,
				LastUpdated:    now,
				UpdateCount:    1,
				PrevFinalCents: item.FinalPriceCents,
				QuoteStatus:    QuoteStatusLoading,
			}
			continue
		}

		changes := compareSKU(existing.SKU, item)
		entry := &Entry{
			SKU:             item,
			FirstSeen:       existing.FirstSeen,
			LastUpdated:     now,
			UpdateCount:     existing.UpdateCount + 1,
			PrevFinalCents:  existing.FinalPriceCents,
			FinalDeltaCents: item.FinalPriceCents - existing.FinalPriceCents,
			Changes:         changes,
			QuoteStatus:     QuoteStatusLoading,
		}
		for _, change := range changes {
			switch change.Category {
			case CategoryPrice:
				entry.PriceChanged = true
			case CategoryStock:
				entry.StockChanged = true
			case CategoryPromo:
				entry.PromoChanged = true
			case CategoryGift:
				entry.GiftChanged = true
			}
		}
		nextEntries[item.ItemID] = entry
		if len(changes) > 0 {
			changed++
			changedEntries = append(changedEntries, *entry)
		}
	}

	store.entries = nextEntries
	return UpdateResult{Parsed: len(skus), Changed: changed, Total: len(store.entries), ChangedEntries: changedEntries}
}

// ApplyQuote updates display-only quote metadata when the SKU and final price
// still match the cart snapshot that initiated the quote request.
func (store *Store) ApplyQuote(itemID string, finalPriceCents int64, result QuoteResult) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, ok := store.entries[itemID]
	if !ok || entry.FinalPriceCents != finalPriceCents {
		return false
	}
	entry.QuoteStatus = result.Status
	entry.QuoteName = result.Name
	entry.QuoteSpec = result.Spec
	entry.QuotePrice = result.Price
	entry.QuoteTotal = result.Total
	entry.QuoteCost = result.Cost
	entry.QuoteDiff = result.Diff
	entry.QuoteProfitRate = result.ProfitRate
	entry.QuoteError = result.Error
	entry.QuoteUpdatedAt = time.Now()
	return true
}

func compareSKU(oldSKU, newSKU SKU) []Change {
	var changes []Change
	addNumber := func(category, field string, oldValue, newValue int64, money bool) {
		if oldValue == newValue {
			return
		}
		description := ""
		if money {
			delta := newValue - oldValue
			trend := "涨了"
			if delta < 0 {
				trend = "降了"
				delta = -delta
			}
			description = fmt.Sprintf("%s ¥%.2f", trend, float64(delta)/100)
		}
		changes = append(changes, Change{
			Category: category, Field: field,
			Old: strconv.FormatInt(oldValue, 10), New: strconv.FormatInt(newValue, 10),
			Description: description, OldNumber: oldValue, NewNumber: newValue, Numeric: true,
		})
	}
	addText := func(category, field, oldValue, newValue string) {
		if oldValue != newValue {
			changes = append(changes, Change{Category: category, Field: field, Old: oldValue, New: newValue})
		}
	}

	addNumber(CategoryPrice, "原价", oldSKU.PagePriceCents, newSKU.PagePriceCents, true)
	addNumber(CategoryPrice, "到手价", oldSKU.FinalPriceCents, newSKU.FinalPriceCents, true)
	addNumber(CategoryPrice, "优惠额", oldSKU.DiscountCents, newSKU.DiscountCents, true)
	addText(CategoryPrice, "页面价", oldSKU.PricePrim, newSKU.PricePrim)
	addText(CategoryPrice, "显示价", oldSKU.PriceShow, newSKU.PriceShow)
	addText(CategoryPrice, "价格描述", oldSKU.PriceDescription, newSKU.PriceDescription)
	addText(CategoryPrice, "还原价", oldSKU.PriceRevert, newSKU.PriceRevert)

	addText(CategoryStock, "库存状态", oldSKU.StockDesc, newSKU.StockDesc)
	if oldSKU.RemainNum != newSKU.RemainNum && !(oldSKU.RemainNum == -1 && newSKU.RemainNum == -1) {
		addNumber(CategoryStock, "剩余库存", int64(oldSKU.RemainNum), int64(newSKU.RemainNum), false)
	}

	addText(CategoryPromo, "PLUS专享折扣", oldSKU.PlusDiscount, newSKU.PlusDiscount)
	addText(CategoryPromo, "PLUS专享描述", oldSKU.PlusText, newSKU.PlusText)
	compareNamedSet(&changes, CategoryPromo, "促销", promoMap(oldSKU.SelectedPromos), promoMap(newSKU.SelectedPromos))
	addText(CategoryPromo, "降价提示", oldSKU.CutPriceText, newSKU.CutPriceText)
	addNumber(CategoryPromo, "降价幅度", oldSKU.CutPriceCents, newSKU.CutPriceCents, true)
	addText(CategoryPromo, "补贴信息", oldSKU.SubsidyText, newSKU.SubsidyText)

	compareNamedSet(&changes, CategoryGift, "赠品", giftMap(oldSKU.Gifts), giftMap(newSKU.Gifts))
	return changes
}

func promoMap(values []Promo) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		out[firstNonEmpty(value.ID, value.Title)] = value.Title
	}
	return out
}

func giftMap(values []Gift) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		text := value.Name
		if value.Num > 1 {
			text += fmt.Sprintf(" x%d", value.Num)
		}
		out[firstNonEmpty(value.ID, value.Name)] = text
	}
	return out
}

func compareNamedSet(changes *[]Change, category, label string, oldValues, newValues map[string]string) {
	for key, value := range newValues {
		oldValue, ok := oldValues[key]
		switch {
		case !ok:
			*changes = append(*changes, Change{Category: category, Field: "新增" + label, New: value})
		case oldValue != value:
			*changes = append(*changes, Change{Category: category, Field: label + "变更", Old: oldValue, New: value})
		}
	}
	for key, value := range oldValues {
		if _, ok := newValues[key]; !ok {
			*changes = append(*changes, Change{Category: category, Field: "移除" + label, Old: value})
		}
	}
}

// Snapshot returns a copy of every stored SKU, most recently updated first.
func (store *Store) Snapshot() Snapshot {
	store.mu.Lock()
	defer store.mu.Unlock()

	entries := make([]Entry, 0, len(store.entries))
	for _, entry := range store.entries {
		entries = append(entries, *entry)
	}
	sort.Slice(entries, func(left, right int) bool {
		if !entries[left].LastUpdated.Equal(entries[right].LastUpdated) {
			return entries[left].LastUpdated.After(entries[right].LastUpdated)
		}
		return entries[left].ItemID < entries[right].ItemID
	})

	return Snapshot{
		Entries:    entries,
		UpdatedAt:  store.updatedAt,
		ParseCount: store.parseCount,
		TotalSKU:   len(entries),
	}
}

// LoadSnapshot replaces the store contents with a previously persisted
// snapshot. It is used by the desktop app to keep the SKU panel populated after
// a restart.
func (store *Store) LoadSnapshot(snapshot Snapshot) {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.entries = make(map[string]*Entry, len(snapshot.Entries))
	for index := range snapshot.Entries {
		entry := snapshot.Entries[index]
		if strings.TrimSpace(entry.ItemID) == "" {
			continue
		}
		copyEntry := entry
		store.entries[copyEntry.ItemID] = &copyEntry
	}
	store.parseCount = snapshot.ParseCount
	store.updatedAt = snapshot.UpdatedAt
}

// Reset clears all stored SKUs and counters.
func (store *Store) Reset() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries = make(map[string]*Entry)
	store.parseCount = 0
	store.updatedAt = time.Time{}
}
