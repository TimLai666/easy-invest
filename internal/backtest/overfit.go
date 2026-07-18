package backtest

// 回測過擬合診斷改用 Insyra v0.3.0 的 quant 與 stats 套件
// （本專案先前開的 issue #175–#178 已由 Insyra 上游實作）：
//   - SharpePerObservation / AnnualizedSharpe → quant.SharpeRatio
//   - ProbabilityOfBacktestOverfitting        → quant.PBO（CSCV）
//   - DeflatedSharpeRatio                      → quant.DeflatedSharpeRatio + stats.NormCDF/PPF
//
// 這裡保留本專案既有的 []float64 導向函式簽名，只把實作委派給 Insyra，
// 讓 walkforward 與既有測試維持不變；Insyra 內部的夏普採樣本標準差（ddof=1）。

import (
	"math"

	"github.com/HazelnutParadise/insyra"
	"github.com/HazelnutParadise/insyra/quant"
	gostat "gonum.org/v1/gonum/stat"
)

// SharpePerObservation 回傳每期（非年化）夏普比率（quant.SharpeRatio，periodsPerYear=1）。
func SharpePerObservation(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	sr, err := quant.SharpeRatio(insyra.NewDataList(returns), 0, 1)
	if err != nil || math.IsNaN(sr) {
		return 0
	}
	return sr
}

// AnnualizedSharpe 以每年期數年化每期夏普比率（台股一年約 252 個交易日）。
func AnnualizedSharpe(returns []float64, periodsPerYear float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	sr, err := quant.SharpeRatio(insyra.NewDataList(returns), 0, periodsPerYear)
	if err != nil || math.IsNaN(sr) {
		return 0
	}
	return sr
}

// ProbabilityOfBacktestOverfitting 以 CSCV（組合對稱交叉驗證）估計 PBO。
//
// trialReturns[n] 是第 n 個參數組合的對齊每日報酬序列（長度需相同）；
// quant.PBO 要求的是 T×N 矩陣（每欄一個策略、每列一個期別），故每個組合的
// 報酬序列各成一欄。blocks（nSplits）需為偶數且不超過期數。
// 回傳 (pbo, ok)；資料不足或計算失敗時 ok=false。
func ProbabilityOfBacktestOverfitting(trialReturns [][]float64, blocks int) (float64, bool) {
	if len(trialReturns) < 2 {
		return 0, false
	}
	cols := make([]*insyra.DataList, len(trialReturns))
	for j, series := range trialReturns {
		cols[j] = insyra.NewDataList(series)
	}
	pbo, err := quant.PBO(insyra.NewDataTable(cols...), blocks)
	if err != nil || math.IsNaN(pbo) {
		return 0, false
	}
	return pbo, true
}

// DeflatedSharpeRatio 回傳通縮夏普比率（機率 0~1）與觀察到的年化夏普。
//
// returns 是被選中策略的每日報酬；trialSharpes 是所有嘗試過的參數組合各自的
// 每期夏普。委派給 quant.DeflatedSharpeRatio（skew/kurt 以 gonum 計算，
// kurt 轉為非超額峰度，常態=3）。
func DeflatedSharpeRatio(returns []float64, trialSharpes []float64) (dsr, annualized float64, ok bool) {
	if len(returns) < 8 || len(trialSharpes) < 1 {
		return 0, 0, false
	}
	observedSR := SharpePerObservation(returns)
	skew := gostat.Skew(returns, nil)
	kurt := gostat.ExKurtosis(returns, nil) + 3
	d, err := quant.DeflatedSharpeRatio(observedSR, len(returns), skew, kurt, insyra.NewDataList(trialSharpes))
	if err != nil || math.IsNaN(d) {
		return 0, 0, false
	}
	return d, observedSR * math.Sqrt(252), true
}

// argmax 回傳最大元素索引（walk-forward 選整段樣本內最佳組合用）。
func argmax(xs []float64) int {
	best := 0
	for i := 1; i < len(xs); i++ {
		if xs[i] > xs[best] {
			best = i
		}
	}
	return best
}
