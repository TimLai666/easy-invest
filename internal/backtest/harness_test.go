package backtest

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

func day(value string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return t
}

func dec(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func testAssets() map[string]AssetMeta {
	return map[string]AssetMeta{
		"0050":  {AssetID: "asset-0050", AssetType: twmarket.AssetTWETF, LotSize: 1000},
		"00878": {AssetID: "asset-00878", AssetType: twmarket.AssetTWETF, LotSize: 1000},
	}
}

func testSettings() strategy.Settings {
	return strategy.Settings{
		TargetWeights: map[string]decimal.Decimal{
			"0050":  dec("0.4"),
			"00878": dec("0.4"),
		},
		RebalanceBand:  dec("0.005"),
		MinTradeAmount: decimal.NewFromInt(500),
	}
}

// M6 驗收核心：回測模擬最後一個再平衡日的建議輸出，
// 必須等於直接用同日資料呼叫 strategy.Rebalance 的輸出。
func TestRunFinalRebalanceMatchesDirectStrategyCall(t *testing.T) {
	bars := map[string][]Bar{
		"0050": {
			{Date: day("2024-01-02"), Close: dec("100")},
			{Date: day("2024-01-03"), Close: dec("102")},
			{Date: day("2024-02-01"), Close: dec("110")},
		},
		"00878": {
			{Date: day("2024-01-02"), Close: dec("20")},
			{Date: day("2024-01-03"), Close: dec("21")},
			{Date: day("2024-02-01"), Close: dec("22")},
		},
	}
	fees := twmarket.DefaultFeeSettings()
	result, err := Simulate(Input{
		Bars:        bars,
		Assets:      testAssets(),
		InitialCash: decimal.NewFromInt(100000),
		Settings:    testSettings(),
		Fees:        fees,
		Execution:   Execution{FillTiming: "close"}, // 釘住收盤成交以驗證手算數值
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	// 只有 1/2 與 2/1 是每月第一個交易日。
	if len(result.Rebalances) != 2 {
		t.Fatalf("rebalances = %d, want 2", len(result.Rebalances))
	}
	last := result.Rebalances[len(result.Rebalances)-1]
	if got := dateKey(last.Date); got != "2024-02-01" {
		t.Fatalf("last rebalance date = %s, want 2024-02-01", got)
	}

	// 手算 1/2 再平衡後狀態：
	// 買 0050 400 股（40000 + 手續費 57）、買 00878 2000 股（40000 + 57），
	// 現金 100000 − 40057 − 40057 = 19886。
	directSettings := testSettings()
	directSettings.Fees = fees
	direct := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(19886),
		Positions: []strategy.Position{
			{Symbol: "0050", AssetID: "asset-0050", AssetType: twmarket.AssetTWETF, LotSize: 1000,
				QuantityShares: decimal.NewFromInt(400), Price: decimal.NewFromInt(110)},
			{Symbol: "00878", AssetID: "asset-00878", AssetType: twmarket.AssetTWETF, LotSize: 1000,
				QuantityShares: decimal.NewFromInt(2000), Price: decimal.NewFromInt(22)},
		},
		Settings: directSettings,
	})
	assertSameIntents(t, direct, last.Intents)
}

// 手算整段引擎行為：成交、費稅、權益曲線與期末持倉。
func TestRunHandCalculatedEngineState(t *testing.T) {
	bars := map[string][]Bar{
		"0050": {
			{Date: day("2024-01-02"), Close: dec("100")},
			{Date: day("2024-01-03"), Close: dec("102")},
			{Date: day("2024-02-01"), Close: dec("110")},
		},
		"00878": {
			{Date: day("2024-01-02"), Close: dec("20")},
			{Date: day("2024-01-03"), Close: dec("21")},
			{Date: day("2024-02-01"), Close: dec("22")},
		},
	}
	result, err := Simulate(Input{
		Bars:        bars,
		Assets:      testAssets(),
		InitialCash: decimal.NewFromInt(100000),
		Settings:    testSettings(),
		Fees:        twmarket.DefaultFeeSettings(),
		Execution:   Execution{FillTiming: "close"}, // 釘住收盤成交以驗證手算數值
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	// 1/2：買 0050 400 股（費 57）、00878 2000 股（費 57）→ 現金 19886。
	// 2/1：兩檔皆超過目標 0.4 + 0.005，各賣 7 股與 38 股：
	//   0050 賣 770 元，手續費低消 20、稅 0 → 淨入 750；
	//   00878 賣 836 元，手續費低消 20、稅 0 → 淨入 816；現金 21452。
	if len(result.Trades) != 4 {
		t.Fatalf("trades = %d, want 4", len(result.Trades))
	}
	if want := decimal.NewFromInt(21452); !result.FinalCash.Equal(want) {
		t.Fatalf("final cash = %s, want %s", result.FinalCash, want)
	}
	if want := decimal.NewFromInt(393); !result.FinalPositions["0050"].Equal(want) {
		t.Fatalf("0050 = %s, want %s", result.FinalPositions["0050"], want)
	}
	if want := decimal.NewFromInt(1962); !result.FinalPositions["00878"].Equal(want) {
		t.Fatalf("00878 = %s, want %s", result.FinalPositions["00878"], want)
	}

	wantEquity := []string{"99886", "102686", "107846"}
	if len(result.EquityCurve) != len(wantEquity) {
		t.Fatalf("equity points = %d, want %d", len(result.EquityCurve), len(wantEquity))
	}
	for i, want := range wantEquity {
		if !result.EquityCurve[i].Equity.Equal(dec(want)) {
			t.Fatalf("equity[%d] = %s, want %s", i, result.EquityCurve[i].Equity, want)
		}
	}
	if want := dec("107846").Div(dec("100000")).Sub(decimal.NewFromInt(1)); !result.TotalReturn.Equal(want) {
		t.Fatalf("total return = %s, want %s", result.TotalReturn, want)
	}
	if !result.AnnualizedReturn.GreaterThan(decimal.Zero) {
		t.Fatalf("annualized return = %s, want > 0", result.AnnualizedReturn)
	}
}

// 次日開盤成交：在 T 日收盤決策，但以 T+1 日開盤價成交（避免 look-ahead），
// 並可套用單邊滑價。
func TestRunNextOpenFillUsesNextDayOpenWithSlippage(t *testing.T) {
	// 1/02 是每月第一個交易日（決策日，收盤 100）；1/03 為成交日（開盤 101）。
	bars := map[string][]Bar{
		"0050": {
			{Date: day("2024-01-02"), Open: dec("100"), Close: dec("100")},
			{Date: day("2024-01-03"), Open: dec("101"), Close: dec("105")},
		},
	}
	settings := strategy.Settings{
		TargetWeights:  map[string]decimal.Decimal{"0050": dec("0.4")},
		RebalanceBand:  dec("0.005"),
		MinTradeAmount: decimal.NewFromInt(500),
	}

	// 無滑價：成交價應為次日開盤 101，而非決策日收盤 100。
	noSlip, err := Simulate(Input{
		Bars: bars, Assets: testAssets(), InitialCash: decimal.NewFromInt(100000),
		Settings: settings, Fees: twmarket.DefaultFeeSettings(),
		Execution: Execution{FillTiming: "next_open"},
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if len(noSlip.Trades) != 1 {
		t.Fatalf("trades = %d, want 1", len(noSlip.Trades))
	}
	trade := noSlip.Trades[0]
	if got := dateKey(trade.Date); got != "2024-01-03" {
		t.Fatalf("成交日 = %s, want 2024-01-03（次日）", got)
	}
	if !trade.Price.Equal(dec("101")) {
		t.Fatalf("成交價 = %s, want 101（次日開盤，非決策日收盤 100）", trade.Price)
	}
	if !trade.QuantityShares.Equal(decimal.NewFromInt(400)) {
		t.Fatalf("成交股數 = %s, want 400", trade.QuantityShares)
	}

	// 100 bps 單邊滑價：買進成交價 = 101 × 1.01 = 102.01。
	withSlip, err := Simulate(Input{
		Bars: bars, Assets: testAssets(), InitialCash: decimal.NewFromInt(100000),
		Settings: settings, Fees: twmarket.DefaultFeeSettings(),
		Execution: Execution{FillTiming: "next_open", SlippageBps: decimal.NewFromInt(100)},
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if len(withSlip.Trades) != 1 {
		t.Fatalf("trades = %d, want 1", len(withSlip.Trades))
	}
	if !withSlip.Trades[0].Price.Equal(dec("102.01")) {
		t.Fatalf("含滑價成交價 = %s, want 102.01", withSlip.Trades[0].Price)
	}
}

func assertSameIntents(t *testing.T, want, got []strategy.Intent) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("intents len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.Symbol != g.Symbol || w.AssetID != g.AssetID || w.Action != g.Action ||
			w.Reason != g.Reason || w.Risks != g.Risks || w.Sequence != g.Sequence {
			t.Fatalf("intent[%d] 欄位不一致：want %+v, got %+v", i, w, g)
		}
		if !w.QuantityShares.Equal(g.QuantityShares) || !w.EstimatedPrice.Equal(g.EstimatedPrice) ||
			!w.EstimatedAmount.Equal(g.EstimatedAmount) || !w.CurrentWeight.Equal(g.CurrentWeight) ||
			!w.TargetWeight.Equal(g.TargetWeight) || !w.Confidence.Equal(g.Confidence) {
			t.Fatalf("intent[%d] 數值不一致：want %+v, got %+v", i, w, g)
		}
	}
}
