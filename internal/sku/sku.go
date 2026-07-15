// Package sku parses JD (京东) cartview responses into a normalized SKU list and
// keeps a running store that is updated on every interception, tracking price
// changes between captures.
package sku

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SKU is one normalized cart line extracted from a cartview response.
type SKU struct {
	ItemID          string `json:"itemId"`
	Name            string `json:"name"`
	VendorID        string `json:"vendorId"`
	VendorName      string `json:"vendorName"`
	PagePriceCents  int64  `json:"pagePriceCents"`
	FinalPriceCents int64  `json:"finalPriceCents"`
	DiscountCents   int64  `json:"discountCents"`
	Num             int    `json:"num"`
	StockCode       int    `json:"stockCode"`
	StockDesc       string `json:"stockDesc"`
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
}

// ParseCartview parses a raw cartview JSON body into a flat SKU list.
//
// Strategy (order of preference):
//  1. cartInfo.flatSkus — always present and covers every cart line (not paginated).
//     Prices here are decimal-yuan strings like "13239.00".
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

// fromFlatSkus builds the SKU list from cartInfo.flatSkus (prices are decimal yuan).
func fromFlatSkus(info cartInfo) ([]SKU, error) {
	// Build a vendorID→name lookup from vendorList to decorate flat entries.
	vendorNameByID := map[string]string{}
	for _, v := range info.VendorList {
		if vid := v.BaseInfo.VendorID; vid != "" {
			vendorNameByID[vid] = v.BaseInfo.VendorName
		}
	}

	out := make([]SKU, 0, len(info.FlatSkus))
	for _, item := range info.FlatSkus {
		itemID := string(item.ItemID)
		vendorID := string(item.VendorID)
		if strings.TrimSpace(itemID) == "" {
			continue
		}
		// flatSkus price is decimal yuan; multiply by 100 to get cents.
		page := parseDecimalCents(string(item.Price))
		// flatSkus has no landedPrice separate from price — use price as final.
		final := page
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
			DiscountCents:   0, // flatSkus carries no discount info
			Num:             item.Num,
			StockCode:       stockCode,
			StockDesc:       stockDesc,
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

// Update merges a freshly parsed SKU list into the store, incrementing update
// counters and recording final-price changes against the previously stored value.
func (store *Store) Update(skus []SKU) UpdateResult {
	store.mu.Lock()
	defer store.mu.Unlock()

	now := time.Now()
	store.parseCount++
	store.updatedAt = now
	changed := 0
	var changedEntries []Entry

	for _, item := range skus {
		existing, ok := store.entries[item.ItemID]
		if !ok {
			store.entries[item.ItemID] = &Entry{
				SKU:            item,
				FirstSeen:      now,
				LastUpdated:    now,
				UpdateCount:    1,
				PrevFinalCents: item.FinalPriceCents,
			}
			continue
		}

		if existing.FinalPriceCents != item.FinalPriceCents {
			existing.PrevFinalCents = existing.FinalPriceCents
			existing.FinalDeltaCents = item.FinalPriceCents - existing.FinalPriceCents
			existing.PriceChanged = true
			changed++
		} else {
			existing.PriceChanged = false
		}
		existing.SKU = item
		existing.LastUpdated = now
		existing.UpdateCount++
		if existing.PriceChanged {
			changedEntries = append(changedEntries, *existing)
		}
	}

	return UpdateResult{Parsed: len(skus), Changed: changed, Total: len(store.entries), ChangedEntries: changedEntries}
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
