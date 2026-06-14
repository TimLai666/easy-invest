package backtest

import (
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// ParamCandidate 是 walk-forward 要掃描的單一參數組合。
// 目前以再平衡區間（RebalanceBand）為主要可調參數；
// CashBuffer / MinTradeAmount 為選填，大於 0 才覆寫基底設定。
type ParamCandidate struct {
	Label          string
	RebalanceBand  decimal.Decimal
	CashBuffer     decimal.Decimal
	MinTradeAmount decimal.Decimal
}

func (c ParamCandidate) apply(base strategy.Settings) strategy.Settings {
	s := base
	if c.RebalanceBand.GreaterThan(decimal.Zero) {
		s.RebalanceBand = c.RebalanceBand
	}
	if c.CashBuffer.GreaterThan(decimal.Zero) {
		s.CashBuffer = c.CashBuffer
	}
	if c.MinTradeAmount.GreaterThan(decimal.Zero) {
		s.MinTradeAmount = c.MinTradeAmount
	}
	return s
}

// WindowSpec 定義訓練／測試視窗（以「月」為單位）。
type WindowSpec struct {
	TrainMonths int // 訓練（樣本內）視窗長度
	TestMonths  int // 測試（樣本外）視窗長度
	StepMonths  int // 每次往前滑動的月數；0 視為 TestMonths（測試段不重疊）
}

func (w WindowSpec) withDefaults() WindowSpec {
	if w.TrainMonths <= 0 {
		w.TrainMonths = 24
	}
	if w.TestMonths <= 0 {
		w.TestMonths = 6
	}
	if w.StepMonths <= 0 {
		w.StepMonths = w.TestMonths
	}
	return w
}

// WalkForwardInput 是 walk-forward 驗證的完整輸入。
type WalkForwardInput struct {
	Bars         map[string][]Bar
	Assets       map[string]AssetMeta
	InitialCash  decimal.Decimal
	BaseSettings strategy.Settings // 共用基底（TargetWeights 等）
	Grid         []ParamCandidate  // 要掃描的參數組合（至少 2 組才有過擬合診斷意義）
	Fees         twmarket.FeeSettings
	Execution    Execution
	Window       WindowSpec
	Objective    string // "sharpe"（預設）或 "return"
}

// Fold 是單一 walk-forward 視窗的結果。
type Fold struct {
	TrainFrom   string
	TrainTo     string
	TestFrom    string
	TestTo      string
	ChosenLabel string
	ChosenBand  decimal.Decimal
	ISObjective decimal.Decimal // 樣本內目標值（夏普或報酬）
	OOSReturn   decimal.Decimal // 樣本外報酬
	OOSSharpe   decimal.Decimal // 樣本外年化夏普
	Trades      int
}

// WalkForwardResult 是 walk-forward 的總結，含樣本外績效與過擬合診斷。
type WalkForwardResult struct {
	Folds          []Fold
	OOSReturn      decimal.Decimal // 各測試段複利後的總樣本外報酬
	OOSAnnualized  decimal.Decimal
	OOSSharpe      decimal.Decimal
	OOSMaxDrawdown decimal.Decimal
	OOSEquityCurve []EquityPoint
	// 過擬合診斷（以整段歷史對所有參數組合計算）：
	PBO            decimal.Decimal // 過擬合機率 0~1，越高越糟
	DeflatedSharpe decimal.Decimal // 通縮夏普（機率 0~1），> 0.95 才算顯著
	ObservedSharpe decimal.Decimal // 被選中組合的年化夏普（整段）
	SelectedLabel  string          // 整段樣本內夏普最高的參數組合
	Trials         int
}

// WalkForward 執行 walk-forward 驗證：每個視窗用訓練段挑出最佳參數，
// 再用緊接的測試段（樣本外）評估，最後彙總樣本外績效；同時以整段歷史
// 對所有參數組合計算 PBO 與通縮夏普，量化過擬合風險。
func WalkForward(input WalkForwardInput) (WalkForwardResult, error) {
	if len(input.Bars) == 0 {
		return WalkForwardResult{}, errors.New("walk-forward 需要至少一檔標的的歷史行情")
	}
	if input.InitialCash.LessThanOrEqual(decimal.Zero) {
		return WalkForwardResult{}, errors.New("期初資金必須大於 0")
	}
	if len(input.Grid) == 0 {
		return WalkForwardResult{}, errors.New("walk-forward 需要至少一組參數")
	}
	window := input.Window.withDefaults()
	months := monthsOf(input.Bars)
	if len(months) < window.TrainMonths+window.TestMonths {
		return WalkForwardResult{}, errors.New("歷史長度不足以切出一個訓練＋測試視窗")
	}

	result := WalkForwardResult{Trials: len(input.Grid)}

	// --- 過擬合診斷：整段歷史對每個參數組合各跑一次 ---
	fullReturns := make([][]float64, 0, len(input.Grid))
	fullSharpes := make([]float64, 0, len(input.Grid))
	for _, cand := range input.Grid {
		res, err := input.simulate(input.Bars, cand)
		if err != nil {
			continue
		}
		rets := dailyReturns(res.EquityCurve)
		fullReturns = append(fullReturns, rets)
		sh := SharpePerObservation(rets)
		fullSharpes = append(fullSharpes, sh)
	}
	if len(fullReturns) >= 1 {
		sel := argmax(fullSharpes)
		result.SelectedLabel = input.Grid[sel].Label
		if blocks := chooseBlocks(len(fullReturns[0])); blocks >= 2 && len(fullReturns) >= 2 {
			if pbo, ok := ProbabilityOfBacktestOverfitting(fullReturns, blocks); ok {
				result.PBO = decimal.NewFromFloat(pbo)
			}
		}
		if dsr, ann, ok := DeflatedSharpeRatio(fullReturns[sel], fullSharpes); ok {
			result.DeflatedSharpe = decimal.NewFromFloat(dsr)
			result.ObservedSharpe = decimal.NewFromFloat(ann)
		}
	}

	// --- walk-forward 視窗 ---
	combinedEquity := input.InitialCash
	var combinedReturns []float64
	for start := 0; start+window.TrainMonths+window.TestMonths <= len(months); start += window.StepMonths {
		trainMonths := months[start : start+window.TrainMonths]
		testMonths := months[start+window.TrainMonths : start+window.TrainMonths+window.TestMonths]
		trainSet := monthSet(trainMonths)
		testSet := monthSet(testMonths)
		trainBars := sliceBarsByMonths(input.Bars, trainSet)
		testBars := sliceBarsByMonths(input.Bars, testSet)
		if len(trainBars) == 0 || len(testBars) == 0 {
			continue
		}

		// 樣本內：挑出目標值最高的參數組合。
		var chosen ParamCandidate
		bestObj := decimal.Zero
		found := false
		for _, cand := range input.Grid {
			res, err := input.simulate(trainBars, cand)
			if err != nil {
				continue
			}
			obj := objectiveValue(res, input.Objective)
			if !found || obj.GreaterThan(bestObj) {
				bestObj = obj
				chosen = cand
				found = true
			}
		}
		if !found {
			continue
		}

		// 樣本外：用挑出的參數評估。
		oos, err := input.simulate(testBars, chosen)
		if err != nil {
			continue
		}
		oosReturns := dailyReturns(oos.EquityCurve)
		combinedReturns = append(combinedReturns, oosReturns...)

		// 串接樣本外權益曲線（以目前累積權益為基準等比延伸）。
		scale := combinedEquity.Div(input.InitialCash)
		for _, pt := range oos.EquityCurve {
			result.OOSEquityCurve = append(result.OOSEquityCurve, EquityPoint{
				Date:   pt.Date,
				Equity: pt.Equity.Mul(scale),
			})
		}
		combinedEquity = combinedEquity.Mul(decimal.NewFromInt(1).Add(oos.TotalReturn))

		result.Folds = append(result.Folds, Fold{
			TrainFrom:   trainMonths[0],
			TrainTo:     trainMonths[len(trainMonths)-1],
			TestFrom:    testMonths[0],
			TestTo:      testMonths[len(testMonths)-1],
			ChosenLabel: chosen.Label,
			ChosenBand:  chosen.RebalanceBand,
			ISObjective: bestObj,
			OOSReturn:   oos.TotalReturn,
			OOSSharpe:   decimal.NewFromFloat(AnnualizedSharpe(oosReturns, 252)),
			Trades:      len(oos.Trades),
		})
	}

	if len(result.Folds) == 0 {
		return WalkForwardResult{}, errors.New("依目前視窗設定無法切出任何 walk-forward 視窗")
	}

	result.OOSReturn = combinedEquity.Div(input.InitialCash).Sub(decimal.NewFromInt(1))
	result.OOSSharpe = decimal.NewFromFloat(AnnualizedSharpe(combinedReturns, 252))
	result.OOSMaxDrawdown = maxDrawdown(result.OOSEquityCurve)
	result.OOSAnnualized = annualizedFromCurve(result.OOSEquityCurve, result.OOSReturn)
	return result, nil
}

func (in WalkForwardInput) simulate(bars map[string][]Bar, cand ParamCandidate) (Result, error) {
	return Simulate(Input{
		Bars:        bars,
		Assets:      in.Assets,
		InitialCash: in.InitialCash,
		Settings:    cand.apply(in.BaseSettings),
		Fees:        in.Fees,
		Execution:   in.Execution,
	})
}

func objectiveValue(res Result, objective string) decimal.Decimal {
	if objective == "return" {
		return res.TotalReturn
	}
	return decimal.NewFromFloat(AnnualizedSharpe(dailyReturns(res.EquityCurve), 252))
}

// --- 輔助 ---

func dailyReturns(curve []EquityPoint) []float64 {
	if len(curve) < 2 {
		return nil
	}
	out := make([]float64, 0, len(curve)-1)
	for i := 1; i < len(curve); i++ {
		prev, _ := curve[i-1].Equity.Float64()
		cur, _ := curve[i].Equity.Float64()
		if prev > 0 {
			out = append(out, cur/prev-1)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

func monthsOf(bars map[string][]Bar) []string {
	set := map[string]bool{}
	for _, series := range bars {
		for _, bar := range series {
			set[bar.Date.Format("2006-01")] = true
		}
	}
	months := make([]string, 0, len(set))
	for m := range set {
		months = append(months, m)
	}
	sort.Strings(months)
	return months
}

func monthSet(months []string) map[string]bool {
	set := make(map[string]bool, len(months))
	for _, m := range months {
		set[m] = true
	}
	return set
}

func sliceBarsByMonths(bars map[string][]Bar, months map[string]bool) map[string][]Bar {
	out := map[string][]Bar{}
	for symbol, series := range bars {
		var kept []Bar
		for _, bar := range series {
			if months[bar.Date.Format("2006-01")] {
				kept = append(kept, bar)
			}
		}
		if len(kept) > 0 {
			out[symbol] = kept
		}
	}
	return out
}

func chooseBlocks(length int) int {
	b := 10
	if length < b {
		b = length
	}
	if b%2 != 0 {
		b--
	}
	if b < 2 {
		return 0
	}
	return b
}

func maxDrawdown(curve []EquityPoint) decimal.Decimal {
	peak := decimal.Zero
	maxDD := decimal.Zero
	for _, pt := range curve {
		if pt.Equity.GreaterThan(peak) {
			peak = pt.Equity
		}
		if peak.GreaterThan(decimal.Zero) {
			dd := peak.Sub(pt.Equity).Div(peak)
			if dd.GreaterThan(maxDD) {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func annualizedFromCurve(curve []EquityPoint, totalReturn decimal.Decimal) decimal.Decimal {
	if len(curve) < 2 {
		return totalReturn
	}
	days := int64(curve[len(curve)-1].Date.Sub(curve[0].Date) / (24 * time.Hour))
	growth := decimal.NewFromInt(1).Add(totalReturn)
	if days <= 0 || growth.LessThanOrEqual(decimal.Zero) {
		return totalReturn
	}
	exponent := decimal.NewFromInt(365).Div(decimal.NewFromInt(days))
	if annual, err := growth.PowWithPrecision(exponent, 12); err == nil {
		return annual.Sub(decimal.NewFromInt(1))
	}
	return totalReturn
}
