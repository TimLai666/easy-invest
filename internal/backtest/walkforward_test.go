package backtest

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// buildMonthlyBars 產生 months 個月、每月兩個交易日的合成行情（緩升），
// Open 設為與 Close 相同以利測試可預測。
func buildMonthlyBars(months int, start, step float64) []Bar {
	var bars []Bar
	price := start
	for m := 1; m <= months; m++ {
		for _, dd := range []string{"03", "15"} {
			date := fmt.Sprintf("2023-%02d-%s", m, dd)
			p := decimal.NewFromFloat(price)
			bars = append(bars, Bar{Date: day(date), Open: p, Close: p})
			price += step
		}
	}
	return bars
}

func TestWalkForwardProducesFoldsAndDiagnostics(t *testing.T) {
	bars := map[string][]Bar{
		"0050":  buildMonthlyBars(8, 100, 2),
		"00878": buildMonthlyBars(8, 20, 0.3),
	}
	input := WalkForwardInput{
		Bars:        bars,
		Assets:      testAssets(),
		InitialCash: decimal.NewFromInt(1000000),
		BaseSettings: strategy.Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": dec("0.5"), "00878": dec("0.5")},
			MinTradeAmount: decimal.NewFromInt(500),
		},
		Grid: []ParamCandidate{
			{Label: "band-2%", RebalanceBand: dec("0.02")},
			{Label: "band-5%", RebalanceBand: dec("0.05")},
			{Label: "band-10%", RebalanceBand: dec("0.10")},
		},
		Fees:      twmarket.DefaultFeeSettings(),
		Execution: Execution{FillTiming: "next_open"},
		Window:    WindowSpec{TrainMonths: 3, TestMonths: 1, StepMonths: 1},
		Objective: "sharpe",
	}

	res, err := WalkForward(input)
	if err != nil {
		t.Fatalf("WalkForward: %v", err)
	}
	if len(res.Folds) == 0 {
		t.Fatal("應至少產生一個 walk-forward 視窗")
	}
	if len(res.OOSEquityCurve) == 0 {
		t.Fatal("樣本外權益曲線不應為空")
	}
	if res.SelectedLabel == "" {
		t.Fatal("應選出整段樣本內最佳參數組合")
	}
	if res.Trials != 3 {
		t.Fatalf("Trials = %d, want 3", res.Trials)
	}
	// 診斷值必須落在合理機率範圍。
	if res.PBO.LessThan(decimal.Zero) || res.PBO.GreaterThan(decimal.NewFromInt(1)) {
		t.Fatalf("PBO = %s 不在 [0,1]", res.PBO)
	}
	if res.DeflatedSharpe.LessThan(decimal.Zero) || res.DeflatedSharpe.GreaterThan(decimal.NewFromInt(1)) {
		t.Fatalf("DeflatedSharpe = %s 不在 [0,1]", res.DeflatedSharpe)
	}
	// 每個 fold 都該有挑選的參數與訓練/測試區間。
	for i, f := range res.Folds {
		if f.ChosenLabel == "" {
			t.Fatalf("fold[%d] 未挑選參數", i)
		}
		if f.TrainFrom == "" || f.TestFrom == "" {
			t.Fatalf("fold[%d] 缺訓練或測試區間", i)
		}
	}
}

func TestWalkForwardRejectsTooShortHistory(t *testing.T) {
	bars := map[string][]Bar{"0050": buildMonthlyBars(2, 100, 2)}
	_, err := WalkForward(WalkForwardInput{
		Bars:        bars,
		Assets:      testAssets(),
		InitialCash: decimal.NewFromInt(1000000),
		BaseSettings: strategy.Settings{
			TargetWeights: map[string]decimal.Decimal{"0050": dec("1.0")},
		},
		Grid:   []ParamCandidate{{Label: "band-5%", RebalanceBand: dec("0.05")}},
		Window: WindowSpec{TrainMonths: 3, TestMonths: 1},
	})
	if err == nil {
		t.Fatal("歷史長度不足時應回錯誤")
	}
}
