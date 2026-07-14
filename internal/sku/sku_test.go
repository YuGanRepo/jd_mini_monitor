package sku

import (
	"os"
	"testing"
)

// Realistic flatSkus-style cartview fragment (prices in decimal yuan).
const sampleCartview = `{
  "cartInfo": {
    "vendorList": [
      {"baseInfo": {"vendorId": "8880", "vendorName": "京东超市官方自营"}},
      {"baseInfo": {"vendorId": "1000475228", "vendorName": "贵州茅台"}}
    ],
    "flatSkus": {
      "lk1w1X1633457428718829568": {
        "itemId": "100057489119",
        "itemName": "茅台 2022年 飞天 500ml*6 整箱装",
        "price": "13239.00",
        "itemNum": 1,
        "stockStatus": 0,
				"vendorId": 8880
      },
      "d1S4L2l1623744679097188352": {
        "itemId": "1030721",
        "itemName": "汾酒黄盖玻汾 475mL*12 整箱装",
        "price": "516.00",
        "itemNum": 1,
        "stockStatus": 0,
				"vendorId": 8880
      },
      "F1g4aG1621160543895203840": {
        "itemId": "100087257154",
        "itemName": "国窖1573 整箱",
        "price": "5409.00",
        "itemNum": 1,
        "stockStatus": 1,
		"vendorId": 8880,
        "isNoCheck": true
      }
    }
  }
}`

func TestParseCartviewFlatSkus(t *testing.T) {
	skus, err := ParseCartview([]byte(sampleCartview))
	if err != nil {
		t.Fatalf("ParseCartview() error = %v", err)
	}
	if len(skus) != 3 {
		t.Fatalf("parsed %d skus, want 3", len(skus))
	}

	byID := map[string]SKU{}
	for _, item := range skus {
		byID[item.ItemID] = item
	}

	first := byID["100057489119"]
	if first.ItemID != "100057489119" {
		t.Fatalf("first itemId = %q", first.ItemID)
	}
	if first.PagePriceCents != 1323900 {
		t.Fatalf("page price = %d, want 1323900", first.PagePriceCents)
	}
	if first.FinalPriceCents != 1323900 {
		t.Fatalf("final price = %d, want 1323900", first.FinalPriceCents)
	}
	if first.VendorName != "京东超市官方自营" {
		t.Fatalf("vendorName = %q", first.VendorName)
	}

	// No discount info in flatSkus.
	fenjiu := byID["1030721"]
	if fenjiu.DiscountCents != 0 {
		t.Fatalf("discount = %d, want 0", fenjiu.DiscountCents)
	}
	if fenjiu.FinalPriceCents != 51600 {
		t.Fatalf("final price = %d, want 51600", fenjiu.FinalPriceCents)
	}
	guojiao := byID["100087257154"]
	if guojiao.StockCode != 1 {
		t.Fatalf("stockCode = %d, want 1", guojiao.StockCode)
	}
}

func TestParseCartviewDataWrapper(t *testing.T) {
	wrapped := `{"data": {"cartInfo": {"flatSkus": {"x": {"itemId": "a", "itemName": "n", "price": "1.23"}}}}}`
	skus, err := ParseCartview([]byte(wrapped))
	if err != nil {
		t.Fatalf("ParseCartview() error = %v", err)
	}
	if len(skus) != 1 || skus[0].ItemID != "a" {
		t.Fatalf("unexpected parse result: %+v", skus)
	}
	if skus[0].PagePriceCents != 123 {
		t.Fatalf("price = %d, want 123", skus[0].PagePriceCents)
	}
}

func TestParseCartviewVendorListFallback(t *testing.T) {
	// vendorList only (no flatSkus), cent-format prices.
	vendorOnly := `{
  "cartInfo": {
    "vendorList": [
      {
        "baseInfo": {"vendorId": "1", "vendorName": "test"},
        "items": [
          {"itemId": "x", "itemName": "n", "price": "1323900", "landedPrice": "1310661"}
        ]
      }
    ]
  }
}`
	skus, err := ParseCartview([]byte(vendorOnly))
	if err != nil {
		t.Fatalf("ParseCartview() error = %v", err)
	}
	if len(skus) != 1 {
		t.Fatalf("parsed %d, want 1", len(skus))
	}
	if skus[0].PagePriceCents != 1323900 || skus[0].FinalPriceCents != 1310661 || skus[0].DiscountCents != 13239 {
		t.Fatalf("prices wrong: %+v", skus[0])
	}
}

func TestParseCartviewInvalid(t *testing.T) {
	if _, err := ParseCartview([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if _, err := ParseCartview(nil); err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestParseCartviewFixtureFromEnv(t *testing.T) {
	path := os.Getenv("MINI_PROXY_CARTVIEW_FIXTURE")
	if path == "" {
		t.Skip("MINI_PROXY_CARTVIEW_FIXTURE is not set")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	skus, err := ParseCartview(content)
	if err != nil {
		t.Fatalf("ParseCartview(%s) error = %v", path, err)
	}
	if len(skus) == 0 {
		t.Fatalf("ParseCartview(%s) returned zero SKUs", path)
	}
}

func TestStoreTracksPriceChanges(t *testing.T) {
	store := NewStore()

	first := store.Update([]SKU{{ItemID: "a", Name: "A", FinalPriceCents: 1000, PagePriceCents: 1000}})
	if first.Parsed != 1 || first.Total != 1 || first.Changed != 0 {
		t.Fatalf("first update = %+v", first)
	}

	// Same price again -> no change recorded.
	second := store.Update([]SKU{{ItemID: "a", Name: "A", FinalPriceCents: 1000, PagePriceCents: 1000}})
	if second.Changed != 0 {
		t.Fatalf("expected no change, got %+v", second)
	}

	// Price drops -> change recorded with delta.
	third := store.Update([]SKU{{ItemID: "a", Name: "A", FinalPriceCents: 900, PagePriceCents: 1000}})
	if third.Changed != 1 {
		t.Fatalf("expected one change, got %+v", third)
	}

	snapshot := store.Snapshot()
	if snapshot.TotalSKU != 1 || snapshot.ParseCount != 3 {
		t.Fatalf("snapshot meta wrong: %+v", snapshot)
	}
	entry := snapshot.Entries[0]
	if !entry.PriceChanged {
		t.Fatal("expected PriceChanged=true after drop")
	}
	if entry.PrevFinalCents != 1000 {
		t.Fatalf("prev final = %d, want 1000", entry.PrevFinalCents)
	}
	if entry.FinalDeltaCents != -100 {
		t.Fatalf("delta = %d, want -100", entry.FinalDeltaCents)
	}
	if entry.UpdateCount != 3 {
		t.Fatalf("update count = %d, want 3", entry.UpdateCount)
	}
}

func TestStoreLoadSnapshot(t *testing.T) {
	store := NewStore()
	store.LoadSnapshot(Snapshot{
		Entries: []Entry{
			{
				SKU:         SKU{ItemID: "a", Name: "A", FinalPriceCents: 1234},
				UpdateCount: 2,
			},
		},
		ParseCount: 4,
	})

	snapshot := store.Snapshot()
	if snapshot.TotalSKU != 1 {
		t.Fatalf("total = %d, want 1", snapshot.TotalSKU)
	}
	if snapshot.ParseCount != 4 {
		t.Fatalf("parseCount = %d, want 4", snapshot.ParseCount)
	}
	if snapshot.Entries[0].ItemID != "a" || snapshot.Entries[0].FinalPriceCents != 1234 {
		t.Fatalf("entry restored incorrectly: %+v", snapshot.Entries[0])
	}
}
