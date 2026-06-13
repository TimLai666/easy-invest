package ledger

// 純計算引擎測試：不需要資料庫。
// 驗收算例出自 docs/tw-market-rules.md「計算範例」一節
//（設定：手續費折扣 0.6、低消 20 元；費稅金額直接帶入算例算好的數字，
// 費稅公式本身已在 internal/twmarket 測試）。

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func d(t *testing.T, value string) decimal.Decimal {
	t.Helper()
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		t.Fatalf("無法解析 decimal %q: %v", value, err)
	}
	return parsed
}

func newLot(t *testing.T, originalQty, remainingQty, originalCost, adjustedCost string) *lotState {
	t.Helper()
	return &lotState{
		OriginalQuantity:  d(t, originalQty),
		RemainingQuantity: d(t, remainingQty),
		OriginalCost:      d(t, originalCost),
		AdjustedCost:      d(t, adjustedCost),
	}
}

// effectiveCost 是 portfolio 視角的有效成本：cost × remaining/original。
func effectiveCost(cost, remaining, original decimal.Decimal) decimal.Decimal {
	return cost.Mul(remaining).Div(original)
}

func assertDecimal(t *testing.T, name string, got, want decimal.Decimal) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", name, got.String(), want.String())
	}
}

// 驗收算例 1+3：買進一張 2330 @1000（總成本 1,000,855），賣 500 股 @1,100。
// 淨收入 = 550,000 − 470 − 1,650 = 547,880；FIFO 沖銷成本 500,427.5；
// 已實現損益 47,452.5。
func TestConsumeLotsFIFOAcceptanceExample3(t *testing.T) {
	lot := newLot(t, "1000", "1000", "1000855", "1000855")
	consumptions, err := consumeLotsFIFO([]*lotState{lot}, d(t, "500"), d(t, "547880"))
	if err != nil {
		t.Fatal(err)
	}
	if len(consumptions) != 1 {
		t.Fatalf("沖銷筆數 = %d, want 1", len(consumptions))
	}
	assertDecimal(t, "quantity", consumptions[0].Quantity, d(t, "500"))
	assertDecimal(t, "cost_consumed", consumptions[0].CostConsumed, d(t, "500427.5"))
	assertDecimal(t, "realized_pnl", consumptions[0].RealizedPnL, d(t, "47452.5"))
	assertDecimal(t, "remaining", lot.RemainingQuantity, d(t, "500"))
	// original_cost 不因沖銷改動（有效成本由 remaining/original 比例呈現）
	assertDecimal(t, "original_cost", lot.OriginalCost, d(t, "1000855"))
	if !lot.Dirty {
		t.Fatal("沖銷後批次應標記 Dirty")
	}
}

func TestConsumeLotsFIFOAcrossLots(t *testing.T) {
	lotA := newLot(t, "1000", "1000", "100000", "100000")
	lotB := newLot(t, "1000", "1000", "90000", "90000")
	consumptions, err := consumeLotsFIFO([]*lotState{lotA, lotB}, d(t, "1500"), d(t, "150000"))
	if err != nil {
		t.Fatal(err)
	}
	if len(consumptions) != 2 {
		t.Fatalf("沖銷筆數 = %d, want 2", len(consumptions))
	}
	// 先進先出：A 整批吃完，B 吃 500
	assertDecimal(t, "A quantity", consumptions[0].Quantity, d(t, "1000"))
	assertDecimal(t, "A cost_consumed", consumptions[0].CostConsumed, d(t, "100000"))
	assertDecimal(t, "A realized", consumptions[0].RealizedPnL, d(t, "0"))
	assertDecimal(t, "B quantity", consumptions[1].Quantity, d(t, "500"))
	assertDecimal(t, "B cost_consumed", consumptions[1].CostConsumed, d(t, "45000"))
	assertDecimal(t, "B realized", consumptions[1].RealizedPnL, d(t, "5000"))
	assertDecimal(t, "A remaining", lotA.RemainingQuantity, d(t, "0"))
	assertDecimal(t, "B remaining", lotB.RemainingQuantity, d(t, "500"))
}

func TestConsumeLotsFIFOInsufficientShares(t *testing.T) {
	lot := newLot(t, "1000", "1000", "100000", "100000")
	if _, err := consumeLotsFIFO([]*lotState{lot}, d(t, "2500"), d(t, "250000")); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("err = %v, want ErrInsufficientShares", err)
	}
}

func TestApplySplitToLots(t *testing.T) {
	cases := []struct {
		name          string
		ratio         string
		lot           [4]string // originalQty, remainingQty, originalCost, adjustedCost
		wantOriginal  string
		wantRemaining string
		wantNetNew    string
	}{
		{"一拆二", "2", [4]string{"1000", "1000", "100000", "100000"}, "2000", "2000", "1000"},
		{"二併一（反分割）", "0.5", [4]string{"1000", "1000", "100000", "100000"}, "500", "500", "-500"},
		{"部分沖銷過的批次", "2", [4]string{"1000", "400", "100000", "80000"}, "2000", "800", "400"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lot := newLot(t, tc.lot[0], tc.lot[1], tc.lot[2], tc.lot[3])
			effOriginalBefore := effectiveCost(lot.OriginalCost, lot.RemainingQuantity, lot.OriginalQuantity)
			effAdjustedBefore := effectiveCost(lot.AdjustedCost, lot.RemainingQuantity, lot.OriginalQuantity)
			netNew, err := applySplitToLots([]*lotState{lot}, d(t, tc.ratio))
			if err != nil {
				t.Fatal(err)
			}
			assertDecimal(t, "net_new", netNew, d(t, tc.wantNetNew))
			assertDecimal(t, "original_quantity", lot.OriginalQuantity, d(t, tc.wantOriginal))
			assertDecimal(t, "remaining_quantity", lot.RemainingQuantity, d(t, tc.wantRemaining))
			// 總成本不變，有效成本在等比縮放下也不變
			assertDecimal(t, "original_cost", lot.OriginalCost, d(t, tc.lot[2]))
			assertDecimal(t, "adjusted_cost", lot.AdjustedCost, d(t, tc.lot[3]))
			assertDecimal(t, "有效原始成本", effectiveCost(lot.OriginalCost, lot.RemainingQuantity, lot.OriginalQuantity), effOriginalBefore)
			assertDecimal(t, "有效調整後成本", effectiveCost(lot.AdjustedCost, lot.RemainingQuantity, lot.OriginalQuantity), effAdjustedBefore)
		})
	}

	t.Run("已平倉批次不動", func(t *testing.T) {
		closed := newLot(t, "1000", "0", "100000", "100000")
		netNew, err := applySplitToLots([]*lotState{closed}, d(t, "2"))
		if err != nil {
			t.Fatal(err)
		}
		assertDecimal(t, "net_new", netNew, d(t, "0"))
		assertDecimal(t, "original_quantity", closed.OriginalQuantity, d(t, "1000"))
		if closed.Dirty {
			t.Fatal("已平倉批次不應被改動")
		}
	})

	t.Run("非法比率", func(t *testing.T) {
		for _, ratio := range []string{"0", "-1", "1"} {
			lot := newLot(t, "1000", "1000", "100000", "100000")
			if _, err := applySplitToLots([]*lotState{lot}, d(t, ratio)); !errors.Is(err, ErrValidation) {
				t.Fatalf("ratio=%s err = %v, want ErrValidation", ratio, err)
			}
		}
	})
}

// 分割後賣出的 FIFO 正確性：1,000 股成本 100,000，一拆二後賣 1,500 股。
func TestSplitThenSellFIFO(t *testing.T) {
	lot := newLot(t, "1000", "1000", "100000", "100000")
	if _, err := applySplitToLots([]*lotState{lot}, d(t, "2")); err != nil {
		t.Fatal(err)
	}
	consumptions, err := consumeLotsFIFO([]*lotState{lot}, d(t, "1500"), d(t, "90000"))
	if err != nil {
		t.Fatal(err)
	}
	// 分割後每股成本 50：沖銷成本 = 100000 × 1500/2000 = 75000
	assertDecimal(t, "cost_consumed", consumptions[0].CostConsumed, d(t, "75000"))
	assertDecimal(t, "realized_pnl", consumptions[0].RealizedPnL, d(t, "15000"))
	assertDecimal(t, "remaining", lot.RemainingQuantity, d(t, "500"))
	assertDecimal(t, "剩餘有效原始成本", effectiveCost(lot.OriginalCost, lot.RemainingQuantity, lot.OriginalQuantity), d(t, "25000"))
}

// 驗收算例 4（adjusted_cost 視角）：持有 2,000 股、實收股利 23,484
//（gross 24,000 − 健保 506 − 匯費 10），調整後成本扣 23,484、原始成本不動。
func TestDeductAdjustedCostDividend(t *testing.T) {
	lot := newLot(t, "2000", "2000", "200171", "200171")
	deductAdjustedCostProRata([]*lotState{lot}, d(t, "23484"))
	assertDecimal(t, "adjusted_cost", lot.AdjustedCost, d(t, "176687"))
	assertDecimal(t, "original_cost", lot.OriginalCost, d(t, "200171"))
}

// 多批次按未平倉股數比例分攤；部分沖銷過的批次以 original_quantity 基準寫回，
// 有效扣減合計必須等於股利金額。
func TestDeductAdjustedCostProRataAcrossLots(t *testing.T) {
	lotA := newLot(t, "1000", "1000", "100000", "100000")
	lotB := newLot(t, "2000", "500", "160000", "80000")
	closed := newLot(t, "500", "0", "50000", "50000")
	lots := []*lotState{lotA, lotB, closed}

	effBefore := effectiveCost(lotA.AdjustedCost, lotA.RemainingQuantity, lotA.OriginalQuantity).
		Add(effectiveCost(lotB.AdjustedCost, lotB.RemainingQuantity, lotB.OriginalQuantity))

	// 未平倉合計 1,500 股，每股扣 1 元
	deductAdjustedCostProRata(lots, d(t, "1500"))

	assertDecimal(t, "A adjusted_cost", lotA.AdjustedCost, d(t, "99000"))
	assertDecimal(t, "B adjusted_cost", lotB.AdjustedCost, d(t, "78000"))
	assertDecimal(t, "closed adjusted_cost", closed.AdjustedCost, d(t, "50000"))

	effAfter := effectiveCost(lotA.AdjustedCost, lotA.RemainingQuantity, lotA.OriginalQuantity).
		Add(effectiveCost(lotB.AdjustedCost, lotB.RemainingQuantity, lotB.OriginalQuantity))
	assertDecimal(t, "有效扣減合計", effBefore.Sub(effAfter), d(t, "1500"))

	// 原始成本三批都不動
	assertDecimal(t, "A original_cost", lotA.OriginalCost, d(t, "100000"))
	assertDecimal(t, "B original_cost", lotB.OriginalCost, d(t, "160000"))
}

func TestDeductAdjustedCostNoOpenLots(t *testing.T) {
	closed := newLot(t, "1000", "0", "100000", "100000")
	deductAdjustedCostProRata([]*lotState{closed}, d(t, "500"))
	assertDecimal(t, "adjusted_cost", closed.AdjustedCost, d(t, "100000"))
}

// 買 → 除息 → 賣 的雙視角數字：
// 買 2,000 股成本 100,000；實收股利 23,484 → 調整後成本 76,516；
// 賣 1,000 股淨收入 60,000 → 已實現損益用原始視角 = 60,000 − 50,000 = 10,000；
// 剩餘 1,000 股：有效原始成本 50,000、有效調整後成本 38,258。
func TestBuyDividendSellDualView(t *testing.T) {
	lot := newLot(t, "2000", "2000", "100000", "100000")
	deductAdjustedCostProRata([]*lotState{lot}, d(t, "23484"))
	assertDecimal(t, "除息後 adjusted_cost", lot.AdjustedCost, d(t, "76516"))

	consumptions, err := consumeLotsFIFO([]*lotState{lot}, d(t, "1000"), d(t, "60000"))
	if err != nil {
		t.Fatal(err)
	}
	assertDecimal(t, "realized_pnl（原始視角）", consumptions[0].RealizedPnL, d(t, "10000"))
	assertDecimal(t, "剩餘有效原始成本", effectiveCost(lot.OriginalCost, lot.RemainingQuantity, lot.OriginalQuantity), d(t, "50000"))
	assertDecimal(t, "剩餘有效調整後成本", effectiveCost(lot.AdjustedCost, lot.RemainingQuantity, lot.OriginalQuantity), d(t, "38258"))
}

func TestConsumeLotsForAdjustment(t *testing.T) {
	t.Run("預設記 FIFO 比例成本、損益為零", func(t *testing.T) {
		lot := newLot(t, "1000", "1000", "100000", "100000")
		consumptions, err := consumeLotsForAdjustment([]*lotState{lot}, d(t, "200"), nil)
		if err != nil {
			t.Fatal(err)
		}
		assertDecimal(t, "cost_consumed", consumptions[0].CostConsumed, d(t, "20000"))
		assertDecimal(t, "realized_pnl", consumptions[0].RealizedPnL, d(t, "0"))
		assertDecimal(t, "remaining", lot.RemainingQuantity, d(t, "800"))
	})
	t.Run("指定 unit_cost", func(t *testing.T) {
		lot := newLot(t, "1000", "1000", "100000", "100000")
		unitCost := d(t, "50")
		consumptions, err := consumeLotsForAdjustment([]*lotState{lot}, d(t, "200"), &unitCost)
		if err != nil {
			t.Fatal(err)
		}
		assertDecimal(t, "cost_consumed", consumptions[0].CostConsumed, d(t, "10000"))
		assertDecimal(t, "realized_pnl", consumptions[0].RealizedPnL, d(t, "0"))
	})
	t.Run("股數不足", func(t *testing.T) {
		lot := newLot(t, "100", "100", "10000", "10000")
		if _, err := consumeLotsForAdjustment([]*lotState{lot}, d(t, "200"), nil); !errors.Is(err, ErrInsufficientShares) {
			t.Fatalf("err = %v, want ErrInsufficientShares", err)
		}
	})
}

// 驗收算例 5：持有 2,000 股、配股 300 股零成本批次。
// 平均成本由 X/2000 變成 X/2300，調整後視角不需額外動作。
func TestStockDividendZeroCostLot(t *testing.T) {
	main := newLot(t, "2000", "2000", "200000", "200000")
	dividend := newLot(t, "300", "300", "0", "0")
	lots := []*lotState{main, dividend}

	totalShares := sumRemainingShares(lots)
	totalCost := decimal.Zero
	for _, lot := range lots {
		totalCost = totalCost.Add(effectiveCost(lot.OriginalCost, lot.RemainingQuantity, lot.OriginalQuantity))
	}
	assertDecimal(t, "總股數", totalShares, d(t, "2300"))
	assertDecimal(t, "總成本（不變）", totalCost, d(t, "200000"))
	avgBefore := d(t, "200000").Div(d(t, "2000"))
	avgAfter := totalCost.Div(totalShares)
	if !avgAfter.LessThan(avgBefore) {
		t.Fatalf("配股後平均成本應下降：%s -> %s", avgBefore, avgAfter)
	}
	if got, want := avgAfter.Round(2).String(), "86.96"; got != want {
		t.Fatalf("平均成本 = %s, want %s", got, want)
	}
}

func TestMetadataDecimal(t *testing.T) {
	cases := []struct {
		name    string
		md      map[string]any
		wantOK  bool
		wantErr bool
		want    string
	}{
		{"字串數字", map[string]any{"k": "2.5"}, true, false, "2.5"},
		{"負數字串", map[string]any{"k": "-30"}, true, false, "-30"},
		{"JSON 數字（float64）", map[string]any{"k": float64(0.5)}, true, false, "0.5"},
		{"int", map[string]any{"k": int(3)}, true, false, "3"},
		{"鍵不存在", map[string]any{}, false, false, ""},
		{"nil map", nil, false, false, ""},
		{"非法字串", map[string]any{"k": "abc"}, false, true, ""},
		{"非法型別", map[string]any{"k": true}, false, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := metadataDecimal(tc.md, "k")
			if tc.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok {
				assertDecimal(t, "value", got, d(t, tc.want))
			}
		})
	}
}
