package backtest

import (
	"math"
	"testing"

	"github.com/HazelnutParadise/insyra/stats"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// 我們現在依賴 Insyra 的 stats.NormCDF/NormPPF（本專案 issue #176 上游實作），
// 這裡做一次健全性檢查，確認上游行為符合預期。
func TestInsyraNormHelpers(t *testing.T) {
	if !approx(stats.NormCDF(0), 0.5, 1e-9) {
		t.Fatalf("stats.NormCDF(0) = %v, want 0.5", stats.NormCDF(0))
	}
	if !approx(stats.NormCDF(1.96), 0.975, 1e-3) {
		t.Fatalf("stats.NormCDF(1.96) = %v, want ~0.975", stats.NormCDF(1.96))
	}
	ppf0, err := stats.NormPPF(0.5)
	if err != nil || !approx(ppf0, 0, 1e-6) {
		t.Fatalf("stats.NormPPF(0.5) = %v, err=%v, want 0", ppf0, err)
	}
	ppf975, err := stats.NormPPF(0.975)
	if err != nil || !approx(ppf975, 1.959964, 1e-3) {
		t.Fatalf("stats.NormPPF(0.975) = %v, err=%v, want ~1.96", ppf975, err)
	}
}

// 一個參數組合在任何子集都嚴格最佳時，PBO 應為 0。
func TestPBODominatingStrategyHasZeroOverfit(t *testing.T) {
	length := 24
	base := make([]float64, length)
	for i := range base {
		if i%2 == 0 {
			base[i] = 0.012
		} else {
			base[i] = -0.004
		}
	}
	trials := make([][]float64, 4)
	for c := 0; c < 4; c++ {
		series := make([]float64, length)
		for i := range series {
			series[i] = base[i] + 0.001*float64(c) // c 越大平均報酬越高、夏普越高
		}
		trials[c] = series
	}
	pbo, ok := ProbabilityOfBacktestOverfitting(trials, 4)
	if !ok {
		t.Fatal("PBO 計算應成功")
	}
	if pbo != 0 {
		t.Fatalf("PBO = %v, want 0（最佳組合在樣本外仍最佳）", pbo)
	}
}

func TestPBOGuards(t *testing.T) {
	if _, ok := ProbabilityOfBacktestOverfitting([][]float64{{0.1, 0.2}}, 2); ok {
		t.Fatal("單一組合不該能算 PBO")
	}
	if _, ok := ProbabilityOfBacktestOverfitting([][]float64{{0.1, -0.1}, {0.2, -0.2}}, 3); ok {
		t.Fatal("奇數 blocks 應回 ok=false")
	}
}

func TestDeflatedSharpeStrongPositiveIsSignificant(t *testing.T) {
	// 平均 0.005、標準差約 0.01 的對稱兩點序列：每期夏普約 0.5。
	returns := make([]float64, 60)
	for i := range returns {
		if i%2 == 0 {
			returns[i] = 0.015
		} else {
			returns[i] = -0.005
		}
	}
	dsr, ann, ok := DeflatedSharpeRatio(returns, []float64{0.5})
	if !ok {
		t.Fatal("DSR 計算應成功")
	}
	if dsr < 0.99 {
		t.Fatalf("DSR = %v, want > 0.99（強正夏普、樣本長）", dsr)
	}
	if ann <= 0 {
		t.Fatalf("年化夏普 = %v, want > 0", ann)
	}
}

func TestDeflatedSharpeZeroMeanIsHalf(t *testing.T) {
	returns := make([]float64, 60)
	for i := range returns {
		if i%2 == 0 {
			returns[i] = 0.01
		} else {
			returns[i] = -0.01
		}
	}
	dsr, _, ok := DeflatedSharpeRatio(returns, []float64{0})
	if !ok {
		t.Fatal("DSR 計算應成功")
	}
	if !approx(dsr, 0.5, 1e-9) {
		t.Fatalf("零平均報酬 DSR = %v, want 0.5", dsr)
	}
}
