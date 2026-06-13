package backtest

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/twmarket"
)

// M6 驗收：定期定額基準兩個月份手算驗證。
// 1 月：價 100，10000 元上限 → 99 股（9900 + 手續費低消 20 = 9920）。
// 2 月：價 110，10000 元上限 → 90 股（9900 + 20 = 9920）。
func TestDCATwoMonthsHandCalc(t *testing.T) {
	result, err := DCA(BenchmarkInput{
		Symbol: "0050",
		Asset:  AssetMeta{AssetID: "asset-0050", AssetType: twmarket.AssetTWETF, LotSize: 1000},
		Bars: []Bar{
			{Date: day("2024-01-02"), Close: dec("100")},
			{Date: day("2024-01-15"), Close: dec("105")},
			{Date: day("2024-02-01"), Close: dec("110")},
			{Date: day("2024-02-15"), Close: dec("108")},
		},
		InitialCash:   decimal.NewFromInt(20000),
		MonthlyAmount: decimal.NewFromInt(10000),
		Fees:          twmarket.DefaultFeeSettings(),
	})
	if err != nil {
		t.Fatalf("DCA: %v", err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("trades = %d, want 2", len(result.Trades))
	}
	first, second := result.Trades[0], result.Trades[1]
	if !first.QuantityShares.Equal(decimal.NewFromInt(99)) || !first.Price.Equal(dec("100")) {
		t.Fatalf("第一筆 = %s 股 @ %s, want 99 @ 100", first.QuantityShares, first.Price)
	}
	if !first.Fee.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("第一筆手續費 = %s, want 低消 20", first.Fee)
	}
	if !second.QuantityShares.Equal(decimal.NewFromInt(90)) || !second.Price.Equal(dec("110")) {
		t.Fatalf("第二筆 = %s 股 @ %s, want 90 @ 110", second.QuantityShares, second.Price)
	}
	// 現金：20000 − 9920 − 9920 = 160；持股 189 股。
	if want := decimal.NewFromInt(160); !result.FinalCash.Equal(want) {
		t.Fatalf("final cash = %s, want %s", result.FinalCash, want)
	}
	if want := decimal.NewFromInt(189); !result.FinalPositions["0050"].Equal(want) {
		t.Fatalf("final shares = %s, want %s", result.FinalPositions["0050"], want)
	}
	// 期末權益：160 + 189×108 = 20572；總報酬 572/20000 = 0.0286。
	if want := dec("20572"); !result.FinalEquity.Equal(want) {
		t.Fatalf("final equity = %s, want %s", result.FinalEquity, want)
	}
	if want := dec("0.0286"); !result.TotalReturn.Equal(want) {
		t.Fatalf("total return = %s, want %s", result.TotalReturn, want)
	}
	// 權益曲線：19980 → 20475 → 20950 → 20572；最大回撤 = 378/20950。
	wantEquity := []string{"19980", "20475", "20950", "20572"}
	for i, want := range wantEquity {
		if !result.EquityCurve[i].Equity.Equal(dec(want)) {
			t.Fatalf("equity[%d] = %s, want %s", i, result.EquityCurve[i].Equity, want)
		}
	}
	if want := dec("378").Div(dec("20950")); !result.MaxDrawdown.Equal(want) {
		t.Fatalf("max drawdown = %s, want %s", result.MaxDrawdown, want)
	}
}

// 買進持有基準：期初一次買進，含手續費不得超過期初資金。
func TestBuyAndHoldHandCalc(t *testing.T) {
	result, err := BuyAndHold(BenchmarkInput{
		Symbol: "0050",
		Asset:  AssetMeta{AssetID: "asset-0050", AssetType: twmarket.AssetTWETF, LotSize: 1000},
		Bars: []Bar{
			{Date: day("2024-01-02"), Close: dec("100")},
			{Date: day("2024-01-03"), Close: dec("110")},
		},
		InitialCash: decimal.NewFromInt(100000),
		Fees:        twmarket.DefaultFeeSettings(),
	})
	if err != nil {
		t.Fatalf("BuyAndHold: %v", err)
	}
	// 1000 股成本 100142 超出資金；998 股 = 99800 + 手續費 142 = 99942。
	if len(result.Trades) != 1 {
		t.Fatalf("trades = %d, want 1", len(result.Trades))
	}
	if want := decimal.NewFromInt(998); !result.Trades[0].QuantityShares.Equal(want) {
		t.Fatalf("quantity = %s, want %s", result.Trades[0].QuantityShares, want)
	}
	if want := decimal.NewFromInt(58); !result.FinalCash.Equal(want) {
		t.Fatalf("final cash = %s, want %s", result.FinalCash, want)
	}
	// 期末權益 58 + 998×110 = 109838。
	if want := dec("109838"); !result.FinalEquity.Equal(want) {
		t.Fatalf("final equity = %s, want %s", result.FinalEquity, want)
	}
}
