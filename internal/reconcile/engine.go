package reconcile

// 對帳差異引擎：純邏輯，不做任何網路、資料庫或檔案 I/O。
// 外層（service.go）先把內部庫存、券商部位、公司行動整理成輸入，再交給這裡計算，
// 回測或其他情境也能重用同一套比對規則。

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// diff_type 定義，與 docs/db-schema.md 的 reconciliation_diffs 六種一致。
const (
	DiffQuantityMismatch        = "quantity_mismatch"         // 兩邊都有但股數不同
	DiffAvgCostMismatch         = "avg_cost_mismatch"         // 券商均價 vs 內部調整後平均成本
	DiffMissingDividend         = "missing_dividend"          // 公司行動有現金股利但 ledger 疑似漏記
	DiffMissingPositionInternal = "missing_position_internal" // 券商有、內部帳上沒有（內部缺漏）
	DiffMissingPositionBroker   = "missing_position_broker"   // 內部有、券商沒有（券商缺部位）
	DiffFeeTaxMismatch          = "fee_tax_mismatch"          // 券商檔案有費稅欄位時的費稅合計差異
)

// AvgCostTolerance 平均成本比對容差：0.01 元。
// 各券商均價捨入方式不一（見 docs/tw-market-rules.md 手續費捨入段落），1 分以內視為一致。
var AvgCostTolerance = decimal.RequireFromString("0.01")

// DividendMatchWindowDays 漏記配息的比對窗口：
// ledger 內 cash_dividend 事件的 trade_date 落在 pay_date 或 ex_date ±45 天內，視為已入帳。
const DividendMatchWindowDays = 45

// InternalPosition 內部帳上的一筆持股，由 ledger 的 Portfolio 與事件加總整理而來。
type InternalPosition struct {
	AssetID        string
	Symbol         string
	QuantityShares decimal.Decimal
	// AdjustedAvgCost 調整後平均成本 = lots.adjusted_cost 依剩餘股數攤提後加總 / 總股數。
	AdjustedAvgCost decimal.Decimal
	// HasAvgCost 股數為零時算不出均價，標記為 false 就跳過均價比對。
	HasAvgCost bool
	// TotalFee / TotalTax 該資產所有未作廢事件的手續費與稅費合計，供 fee_tax_mismatch 比對。
	TotalFee decimal.Decimal
	TotalTax decimal.Decimal
}

// BrokerPositionRow 券商快照中的一筆部位。
type BrokerPositionRow struct {
	Symbol         string
	QuantityShares decimal.Decimal
	// AvgCost 券商成交均價；券商檔案沒給就是 nil，跳過均價比對。
	AvgCost *decimal.Decimal
	// TotalFee / TotalTax 券商檔案有費稅欄位才有值；都為 nil 時不產生 fee_tax_mismatch（不硬湊）。
	TotalFee *decimal.Decimal
	TotalTax *decimal.Decimal
}

// ComputedDiff 引擎輸出的一筆差異，由外層寫入 reconciliation_diffs。
// missing_dividend 的 BrokerValue 放「應收股利估計值」（除息前持股 × 每股股利），InternalValue 留空。
type ComputedDiff struct {
	AssetID       string
	Symbol        string
	DiffType      string
	InternalValue string
	BrokerValue   string
}

// DividendCandidate 公司行動裡的一筆現金股利，外層已算好除息日前的持有股數。
type DividendCandidate struct {
	ActionID     string
	AssetID      string
	Symbol       string
	ExDate       time.Time
	PayDate      *time.Time
	CashPerShare decimal.Decimal
	// HeldShares 除息日前一日帳上持有股數（trade_date < ex_date 的數量加總）。
	HeldShares decimal.Decimal
}

// RecordedDividend ledger 內已存在的未作廢 cash_dividend 事件。
type RecordedDividend struct {
	AssetID   string
	TradeDate time.Time
}

// ComputePositionDiffs 比對內部持股與券商部位，產出部位類差異：
// quantity_mismatch、avg_cost_mismatch、fee_tax_mismatch、
// missing_position_internal（券商有內部沒有）、missing_position_broker（內部有券商沒有）。
func ComputePositionDiffs(internal []InternalPosition, broker []BrokerPositionRow) []ComputedDiff {
	internalBySymbol := map[string]InternalPosition{}
	for _, p := range internal {
		internalBySymbol[p.Symbol] = p
	}
	seen := map[string]bool{}
	var diffs []ComputedDiff
	for _, b := range broker {
		seen[b.Symbol] = true
		p, ok := internalBySymbol[b.Symbol]
		if !ok || p.QuantityShares.LessThanOrEqual(decimal.Zero) {
			// 券商有、內部帳上沒有 → 內部缺漏這筆持股。
			diffs = append(diffs, ComputedDiff{
				AssetID:     p.AssetID,
				Symbol:      b.Symbol,
				DiffType:    DiffMissingPositionInternal,
				BrokerValue: b.QuantityShares.String(),
			})
			continue
		}
		if !p.QuantityShares.Equal(b.QuantityShares) {
			diffs = append(diffs, ComputedDiff{
				AssetID:       p.AssetID,
				Symbol:        p.Symbol,
				DiffType:      DiffQuantityMismatch,
				InternalValue: p.QuantityShares.String(),
				BrokerValue:   b.QuantityShares.String(),
			})
		}
		if b.AvgCost != nil && p.HasAvgCost {
			// 容差 0.01 元：差額「大於」容差才算差異，剛好 0.01 視為一致。
			if p.AdjustedAvgCost.Sub(*b.AvgCost).Abs().GreaterThan(AvgCostTolerance) {
				diffs = append(diffs, ComputedDiff{
					AssetID:       p.AssetID,
					Symbol:        p.Symbol,
					DiffType:      DiffAvgCostMismatch,
					InternalValue: p.AdjustedAvgCost.Round(6).String(),
					BrokerValue:   b.AvgCost.String(),
				})
			}
		}
		if b.TotalFee != nil || b.TotalTax != nil {
			// 券商檔案有費稅欄位才比對費稅合計，缺欄位的那邊以 0 計。
			brokerTotal := decimal.Zero
			if b.TotalFee != nil {
				brokerTotal = brokerTotal.Add(*b.TotalFee)
			}
			if b.TotalTax != nil {
				brokerTotal = brokerTotal.Add(*b.TotalTax)
			}
			internalTotal := p.TotalFee.Add(p.TotalTax)
			if !internalTotal.Equal(brokerTotal) {
				diffs = append(diffs, ComputedDiff{
					AssetID:       p.AssetID,
					Symbol:        p.Symbol,
					DiffType:      DiffFeeTaxMismatch,
					InternalValue: internalTotal.String(),
					BrokerValue:   brokerTotal.String(),
				})
			}
		}
	}
	// 內部有、券商沒有的持股，依代號排序確保輸出穩定。
	var missingAtBroker []InternalPosition
	for _, p := range internal {
		if !seen[p.Symbol] {
			missingAtBroker = append(missingAtBroker, p)
		}
	}
	sort.Slice(missingAtBroker, func(i, j int) bool { return missingAtBroker[i].Symbol < missingAtBroker[j].Symbol })
	for _, p := range missingAtBroker {
		diffs = append(diffs, ComputedDiff{
			AssetID:       p.AssetID,
			Symbol:        p.Symbol,
			DiffType:      DiffMissingPositionBroker,
			InternalValue: p.QuantityShares.String(),
		})
	}
	return diffs
}

// UnmatchedDividends 找出疑似漏記的現金股利。
// 配對規則：每筆股利候選依除息日排序，貪婪配對「同資產、日期最接近、未被其他候選用掉」的
// cash_dividend 事件；事件 trade_date 與 pay_date 或 ex_date 相距 45 天內才算配到。
// 回傳沒配到事件且除息前持股 > 0 的候選。
func UnmatchedDividends(candidates []DividendCandidate, recorded []RecordedDividend) []DividendCandidate {
	sorted := make([]DividendCandidate, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ExDate.Before(sorted[j].ExDate) })

	used := make([]bool, len(recorded))
	var missing []DividendCandidate
	for _, c := range sorted {
		if c.HeldShares.LessThanOrEqual(decimal.Zero) || c.CashPerShare.LessThanOrEqual(decimal.Zero) {
			continue
		}
		best := -1
		bestDist := DividendMatchWindowDays + 1
		for i, ev := range recorded {
			if used[i] || ev.AssetID != c.AssetID {
				continue
			}
			dist := daysApart(ev.TradeDate, c.ExDate)
			if c.PayDate != nil {
				if d := daysApart(ev.TradeDate, *c.PayDate); d < dist {
					dist = d
				}
			}
			if dist <= DividendMatchWindowDays && dist < bestDist {
				best, bestDist = i, dist
			}
		}
		if best >= 0 {
			used[best] = true
			continue
		}
		missing = append(missing, c)
	}
	return missing
}

// ComputeMissingDividends 把 UnmatchedDividends 的結果轉成 missing_dividend 差異。
// BrokerValue 為應收股利估計值 = 除息前持股 × 每股股利。
func ComputeMissingDividends(candidates []DividendCandidate, recorded []RecordedDividend) []ComputedDiff {
	var diffs []ComputedDiff
	for _, c := range UnmatchedDividends(candidates, recorded) {
		diffs = append(diffs, ComputedDiff{
			AssetID:     c.AssetID,
			Symbol:      c.Symbol,
			DiffType:    DiffMissingDividend,
			BrokerValue: EstimatedDividendAmount(c).String(),
		})
	}
	return diffs
}

// EstimatedDividendAmount 一筆股利候選的應收金額估計值。
func EstimatedDividendAmount(c DividendCandidate) decimal.Decimal {
	return c.HeldShares.Mul(c.CashPerShare)
}

// daysApart 兩個時間相距的「日」數（只看日期、忽略時分秒），回傳絕對值。
func daysApart(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	au := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	bu := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	days := int(au.Sub(bu).Hours() / 24)
	if days < 0 {
		return -days
	}
	return days
}
