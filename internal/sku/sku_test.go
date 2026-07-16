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

func TestParsePluginCartPayload(t *testing.T) {
	payload := `{
  "resultData": {"cartInfo": {"vendors": [{
    "vendorId": "88", "shopName": "测试店铺", "sorted": [{"itemType": 1, "item": {
      "skuId": "100001", "skuName": "测试商品*6", "num": 2,
      "Price": "120.00", "landedPrice": "99.00", "Discount": "21.00",
      "pricePrim": "¥120.00", "PriceShow": "¥99.00", "priceDes": "券后", "priceRevert": "¥129.00",
      "stock": {"stockState": "有货", "stockStateId": 33, "remainNumInt": 12},
      "cardPromotionFloor": {"discount": "9.5折", "showText": "PLUS专享"},
      "selectedPromoList": [{"id": "p1", "title": "满100减20"}],
      "cutPriceT": "已降价", "cut": "5.00",
      "giftGroupInfosShow": [{"giftInfos": [{"skuId": "g1", "name": "赠品", "giftNum": 2}]}],
      "skuLabels": {"priceBottom": [{"t": "政府补贴"}]}
    }}]
  }]}}
}`
	skus, err := ParseCartview([]byte(payload))
	if err != nil {
		t.Fatalf("ParseCartview() error = %v", err)
	}
	if len(skus) != 1 {
		t.Fatalf("parsed %d SKUs, want 1", len(skus))
	}
	item := skus[0]
	if item.ItemID != "100001" || item.FinalPriceCents != 9900 || item.PagePriceCents != 12000 || item.DiscountCents != 2100 {
		t.Fatalf("prices or id not parsed: %+v", item)
	}
	if item.RemainNum != 12 || item.StockDesc != "有货" || len(item.SelectedPromos) != 1 || len(item.Gifts) != 1 {
		t.Fatalf("stock/promo/gift not parsed: %+v", item)
	}
	if item.PlusText != "PLUS专享" || item.SubsidyText != "政府补贴" || item.ProductURL == "" || item.CheckoutURL == "" {
		t.Fatalf("metadata not parsed: %+v", item)
	}
}

func TestStoreReplacesSnapshotAndClassifiesChanges(t *testing.T) {
	store := NewStore()
	initial := []SKU{
		{
			ItemID: "a", Name: "A", PagePriceCents: 12000, FinalPriceCents: 10000, DiscountCents: 2000,
			StockDesc: "有货", RemainNum: 20, PlusText: "旧优惠",
			SelectedPromos: []Promo{{ID: "p1", Title: "旧促销"}}, Gifts: []Gift{{ID: "g1", Name: "旧赠品", Num: 1}},
		},
		{ItemID: "removed", Name: "Removed", FinalPriceCents: 5000, RemainNum: -1},
	}
	if result := store.Update(initial); result.Changed != 0 || len(result.ChangedEntries) != 0 {
		t.Fatalf("initial snapshot should not report changes: %+v", result)
	}

	result := store.Update([]SKU{{
		ItemID: "a", Name: "A", PagePriceCents: 11000, FinalPriceCents: 9000, DiscountCents: 2000,
		StockDesc: "无货", RemainNum: 2, PlusText: "新优惠",
		SelectedPromos: []Promo{{ID: "p2", Title: "新促销"}}, Gifts: []Gift{{ID: "g2", Name: "新赠品", Num: 2}},
	}})
	if result.Total != 1 || result.Changed != 1 || len(result.ChangedEntries) != 1 {
		t.Fatalf("replacement update = %+v", result)
	}
	entry := result.ChangedEntries[0]
	if !entry.PriceChanged || !entry.StockChanged || !entry.PromoChanged || !entry.GiftChanged {
		t.Fatalf("missing change category flags: %+v", entry)
	}
	categories := map[string]bool{}
	for _, change := range entry.Changes {
		categories[change.Category] = true
	}
	for _, category := range []string{CategoryPrice, CategoryStock, CategoryPromo, CategoryGift} {
		if !categories[category] {
			t.Fatalf("missing category %q in %+v", category, entry.Changes)
		}
	}
	if snapshot := store.Snapshot(); snapshot.TotalSKU != 1 || snapshot.Entries[0].ItemID != "a" {
		t.Fatalf("removed SKU remained in snapshot: %+v", snapshot)
	}
}
