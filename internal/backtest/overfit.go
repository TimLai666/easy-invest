package backtest

import (
	"math"
	"sort"
)

// 本檔提供回測過擬合的量化診斷，皆為純函式、以 float64 計算統計量
// （診斷指標非金流，毋須 decimal）：
//   - ProbabilityOfBacktestOverfitting：CSCV 法估計的過擬合機率（PBO）。
//   - DeflatedSharpeRatio：校正「多重嘗試選擇偏誤＋非常態＋樣本長度」後，
//     夏普比率顯著大於 0 的機率（DSR）。
// 參考：Bailey & López de Prado, "The Probability of Backtest Overfitting"、
// "The Deflated Sharpe Ratio"。

// SharpePerObservation 回傳每期（非年化）夏普比率：平均報酬 / 母體標準差。
func SharpePerObservation(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	m := mean(returns)
	sd := stddevPop(returns, m)
	if sd == 0 {
		return 0
	}
	return m / sd
}

// AnnualizedSharpe 以每年期數年化每期夏普比率（台股一年約 252 個交易日）。
func AnnualizedSharpe(returns []float64, periodsPerYear float64) float64 {
	return SharpePerObservation(returns) * math.Sqrt(periodsPerYear)
}

// ProbabilityOfBacktestOverfitting 以 CSCV（組合對稱交叉驗證）估計 PBO。
//
// 作法：把對齊的各參數組合報酬序列在時間軸切成 blocks 段（需為偶數），
// 取所有「選一半段當樣本內、另一半當樣本外」的組合；每個組合中找出樣本內
// 夏普最高的參數，看它在樣本外的排名落在中位數以下的比例即為 PBO。
// PBO 越接近 1，代表「樣本內最佳」越常在樣本外變差，過擬合越嚴重。
//
// trialReturns[n] 是第 n 個參數組合的對齊每日報酬序列（長度需相同）。
// 回傳 (pbo, ok)；資料不足以計算時 ok=false。
func ProbabilityOfBacktestOverfitting(trialReturns [][]float64, blocks int) (float64, bool) {
	n := len(trialReturns)
	if n < 2 || blocks < 2 || blocks%2 != 0 {
		return 0, false
	}
	length := len(trialReturns[0])
	for _, r := range trialReturns {
		if len(r) != length {
			return 0, false
		}
	}
	if length < blocks {
		return 0, false
	}

	// 切成 blocks 段連續區間（盡量等長，餘數分給前面幾段）。
	bounds := blockBounds(length, blocks)
	half := blocks / 2
	combos := chooseCombinations(blocks, half)
	if len(combos) == 0 {
		return 0, false
	}

	logitBelow := 0
	total := 0
	for _, isBlocks := range combos {
		isSet := make(map[int]bool, len(isBlocks))
		for _, b := range isBlocks {
			isSet[b] = true
		}
		// 各參數組合在 IS / OOS 段的夏普。
		isSharpe := make([]float64, n)
		oosSharpe := make([]float64, n)
		for cfg := 0; cfg < n; cfg++ {
			var isR, oosR []float64
			for b := 0; b < blocks; b++ {
				seg := trialReturns[cfg][bounds[b]:bounds[b+1]]
				if isSet[b] {
					isR = append(isR, seg...)
				} else {
					oosR = append(oosR, seg...)
				}
			}
			isSharpe[cfg] = SharpePerObservation(isR)
			oosSharpe[cfg] = SharpePerObservation(oosR)
		}
		best := argmax(isSharpe)
		// 樣本內最佳組合在樣本外的相對排名 ω ∈ (0,1)，1=最佳。
		rank := relativeRank(oosSharpe, best)
		lambda := math.Log(rank / (1 - rank))
		if lambda <= 0 {
			logitBelow++
		}
		total++
	}
	if total == 0 {
		return 0, false
	}
	return float64(logitBelow) / float64(total), true
}

// DeflatedSharpeRatio 回傳通縮夏普比率（一個機率 0~1）與觀察到的年化夏普。
//
// returns 是被選中策略的每日報酬；trialSharpes 是所有嘗試過的參數組合各自的
// 「每期夏普」（用來估計選擇偏誤造成的夏普膨脹）。
// DSR 越接近 1（例如 > 0.95）代表即使考量多重嘗試與非常態，夏普仍顯著為正。
func DeflatedSharpeRatio(returns []float64, trialSharpes []float64) (dsr, annualized float64, ok bool) {
	nObs := len(returns)
	if nObs < 8 || len(trialSharpes) < 1 {
		return 0, 0, false
	}
	m := mean(returns)
	sd := stddevPop(returns, m)
	if sd == 0 {
		return 0, 0, false
	}
	sr := m / sd
	sk := skewness(returns, m, sd)
	ku := kurtosis(returns, m, sd) // 常態為 3

	// 期望「最大夏普」基準：在 N 個獨立嘗試下純運氣可達的夏普水準。
	nTrials := float64(len(trialSharpes))
	sigmaSR := stddevSample(trialSharpes)
	var sr0 float64
	if nTrials >= 2 && sigmaSR > 0 {
		const gamma = 0.5772156649015329 // Euler–Mascheroni
		z1 := normPPF(1 - 1/nTrials)
		z2 := normPPF(1 - 1/(nTrials*math.E))
		sr0 = sigmaSR * ((1-gamma)*z1 + gamma*z2)
	}

	denom := 1 - sk*sr + (ku-1)/4*sr*sr
	if denom <= 0 {
		return 0, 0, false
	}
	stat := (sr - sr0) * math.Sqrt(float64(nObs)-1) / math.Sqrt(denom)
	return normCDF(stat), sr * math.Sqrt(252), true
}

// --- 統計輔助（float64） ---

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stddevPop(xs []float64, m float64) float64 {
	if len(xs) < 1 {
		return 0
	}
	var s float64
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)))
}

func stddevSample(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	var s float64
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)-1))
}

func skewness(xs []float64, m, sd float64) float64 {
	if len(xs) == 0 || sd == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		z := (x - m) / sd
		s += z * z * z
	}
	return s / float64(len(xs))
}

func kurtosis(xs []float64, m, sd float64) float64 {
	if len(xs) == 0 || sd == 0 {
		return 3
	}
	var s float64
	for _, x := range xs {
		z := (x - m) / sd
		s += z * z * z * z
	}
	return s / float64(len(xs))
}

func argmax(xs []float64) int {
	best := 0
	for i := 1; i < len(xs); i++ {
		if xs[i] > xs[best] {
			best = i
		}
	}
	return best
}

// relativeRank 回傳 xs[target] 在 xs 中的相對排名 ω ∈ (0,1)，
// 1 代表最高。以 (高於它的數量轉成的名次)/(N+1) 表示，避免 0 與 1。
func relativeRank(xs []float64, target int) float64 {
	n := len(xs)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return xs[idx[a]] < xs[idx[b]] })
	pos := 0 // 由低到高的名次（0-based）
	for r, i := range idx {
		if i == target {
			pos = r
			break
		}
	}
	return float64(pos+1) / float64(n+1)
}

// blockBounds 回傳把 length 切成 blocks 段的邊界索引（長度 blocks+1）。
func blockBounds(length, blocks int) []int {
	bounds := make([]int, blocks+1)
	base := length / blocks
	rem := length % blocks
	idx := 0
	bounds[0] = 0
	for b := 0; b < blocks; b++ {
		size := base
		if b < rem {
			size++
		}
		idx += size
		bounds[b+1] = idx
	}
	return bounds
}

// chooseCombinations 回傳從 0..n-1 取 k 個的所有組合。
func chooseCombinations(n, k int) [][]int {
	var res [][]int
	combo := make([]int, k)
	var rec func(start, depth int)
	rec = func(start, depth int) {
		if depth == k {
			c := make([]int, k)
			copy(c, combo)
			res = append(res, c)
			return
		}
		for i := start; i <= n-(k-depth); i++ {
			combo[depth] = i
			rec(i+1, depth+1)
		}
	}
	rec(0, 0)
	return res
}

func normCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// normPPF 是標準常態分布的反函數（Acklam 近似）。
func normPPF(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}
	a := []float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := []float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := []float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := []float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	plow := 0.02425
	phigh := 1 - plow
	switch {
	case p < plow:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= phigh:
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
}
