package backtest

import (
	"math"
	"testing"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestNormHelpers(t *testing.T) {
	if !approx(normCDF(0), 0.5, 1e-9) {
		t.Fatalf("normCDF(0) = %v, want 0.5", normCDF(0))
	}
	if !approx(normCDF(1.96), 0.975, 1e-3) {
		t.Fatalf("normCDF(1.96) = %v, want ~0.975", normCDF(1.96))
	}
	if !approx(normPPF(0.5), 0, 1e-6) {
		t.Fatalf("normPPF(0.5) = %v, want 0", normPPF(0.5))
	}
	if !approx(normPPF(0.975), 1.959964, 1e-3) {
		t.Fatalf("normPPF(0.975) = %v, want ~1.96", normPPF(0.975))
	}
}

func TestChooseCombinationsCount(t *testing.T) {
	if got := len(chooseCombinations(4, 2)); got != 6 {
		t.Fatalf("C(4,2) = %d, want 6", got)
	}
	if got := len(chooseCombinations(10, 5)); got != 252 {
		t.Fatalf("C(10,5) = %d, want 252", got)
	}
}

func TestBlockBoundsCoverWholeLength(t *testing.T) {
	bounds := blockBounds(23, 4)
	if bounds[0] != 0 || bounds[len(bounds)-1] != 23 {
		t.Fatalf("bounds 邊界錯誤：%v", bounds)
	}
	// 餘數分給前面幾段，段長差距不超過 1。
	for b := 0; b < 4; b++ {
		size := bounds[b+1] - bounds[b]
		if size < 5 || size > 6 {
			t.Fatalf("block %d 長度 %d，預期 5 或 6", b, size)
		}
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
	if _, ok := ProbabilityOfBacktestOverfitting([][]float64{{0.1}, {0.2}}, 3); ok {
		t.Fatal("奇數 blocks 應回 ok=false")
	}
}

func TestDeflatedSharpeStrongPositiveIsSignificant(t *testing.T) {
	// 平均 0.005、標準差 0.01 的對稱兩點序列：每期夏普 0.5。
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
