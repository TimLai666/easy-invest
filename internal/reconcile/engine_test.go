package reconcile

import (
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func dp(value string) *decimal.Decimal {
	dec := decimal.RequireFromString(value)
	return &dec
}

func day(value string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return t
}

func dayPtr(value string) *time.Time {
	t := day(value)
	return &t
}

func TestComputePositionDiffs(t *testing.T) {
	internal2330 := InternalPosition{
		AssetID:         "asset-2330",
		Symbol:          "2330",
		QuantityShares:  d("1000"),
		AdjustedAvgCost: d("600.855"),
		HasAvgCost:      true,
		TotalFee:        d("855"),
		TotalTax:        d("0"),
	}

	tests := []struct {
		name     string
		internal []InternalPosition
		broker   []BrokerPositionRow
		want     []ComputedDiff
	}{
		{
			name:     "兩邊一致時不產生差異",
			internal: []InternalPosition{internal2330},
			broker:   []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.855")}},
			want:     nil,
		},
		{
			name:     "股數不同產生 quantity_mismatch",
			internal: []InternalPosition{internal2330},
			broker:   []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("2000"), AvgCost: dp("600.855")}},
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffQuantityMismatch, InternalValue: "1000", BrokerValue: "2000"},
			},
		},
		{
			name:     "均價差剛好等於容差 0.01 視為一致",
			internal: []InternalPosition{internal2330},
			broker:   []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.865")}},
			want:     nil,
		},
		{
			name:     "均價差超過容差產生 avg_cost_mismatch 且填兩邊數字",
			internal: []InternalPosition{internal2330},
			broker:   []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.866")}},
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffAvgCostMismatch, InternalValue: "600.855", BrokerValue: "600.866"},
			},
		},
		{
			name:     "券商沒給均價就跳過均價比對",
			internal: []InternalPosition{internal2330},
			broker:   []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("1000")}},
			want:     nil,
		},
		{
			name:     "券商有內部沒有產生 missing_position_internal",
			internal: []InternalPosition{internal2330},
			broker: []BrokerPositionRow{
				{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.855")},
				{Symbol: "00878", QuantityShares: d("500")},
			},
			want: []ComputedDiff{
				{Symbol: "00878", DiffType: DiffMissingPositionInternal, BrokerValue: "500"},
			},
		},
		{
			name: "內部有券商沒有產生 missing_position_broker（依代號排序）",
			internal: []InternalPosition{
				internal2330,
				{AssetID: "asset-0050", Symbol: "0050", QuantityShares: d("2000")},
				{AssetID: "asset-0056", Symbol: "0056", QuantityShares: d("3000")},
			},
			broker: []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.855")}},
			want: []ComputedDiff{
				{AssetID: "asset-0050", Symbol: "0050", DiffType: DiffMissingPositionBroker, InternalValue: "2000"},
				{AssetID: "asset-0056", Symbol: "0056", DiffType: DiffMissingPositionBroker, InternalValue: "3000"},
			},
		},
		{
			name:     "券商檔案有費稅欄位且金額不同產生 fee_tax_mismatch",
			internal: []InternalPosition{internal2330},
			broker: []BrokerPositionRow{
				{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.855"), TotalFee: dp("900"), TotalTax: dp("99")},
			},
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffFeeTaxMismatch, InternalValue: "855", BrokerValue: "999"},
			},
		},
		{
			name:     "券商檔案費稅與內部一致時不產生差異",
			internal: []InternalPosition{internal2330},
			broker: []BrokerPositionRow{
				{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.855"), TotalFee: dp("855"), TotalTax: dp("0")},
			},
			want: nil,
		},
		{
			name:     "券商檔案沒有費稅欄位就不比費稅（不硬湊）",
			internal: []InternalPosition{internal2330},
			broker:   []BrokerPositionRow{{Symbol: "2330", QuantityShares: d("1000"), AvgCost: dp("600.855")}},
			want:     nil,
		},
		{
			name:     "同一檔可以同時出現多種差異",
			internal: []InternalPosition{internal2330},
			broker: []BrokerPositionRow{
				{Symbol: "2330", QuantityShares: d("2000"), AvgCost: dp("999.5"), TotalFee: dp("999")},
			},
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffQuantityMismatch, InternalValue: "1000", BrokerValue: "2000"},
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffAvgCostMismatch, InternalValue: "600.855", BrokerValue: "999.5"},
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffFeeTaxMismatch, InternalValue: "855", BrokerValue: "999"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputePositionDiffs(tc.internal, tc.broker)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ComputePositionDiffs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestComputeMissingDividends(t *testing.T) {
	candidate := DividendCandidate{
		ActionID:     "action-1",
		AssetID:      "asset-2330",
		Symbol:       "2330",
		ExDate:       day("2026-03-10"),
		PayDate:      dayPtr("2026-04-15"),
		CashPerShare: d("5"),
		HeldShares:   d("1000"),
	}

	tests := []struct {
		name       string
		candidates []DividendCandidate
		recorded   []RecordedDividend
		want       []ComputedDiff
	}{
		{
			name:       "完全沒有入帳事件時列為疑似漏記，金額為持股×每股股利",
			candidates: []DividendCandidate{candidate},
			recorded:   nil,
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffMissingDividend, BrokerValue: "5000"},
			},
		},
		{
			name:       "事件落在 pay_date ±45 天內視為已入帳",
			candidates: []DividendCandidate{candidate},
			recorded:   []RecordedDividend{{AssetID: "asset-2330", TradeDate: day("2026-05-30")}}, // 距 pay_date 45 天
			want:       nil,
		},
		{
			name:       "事件距 pay_date 與 ex_date 都超過 45 天仍算漏記",
			candidates: []DividendCandidate{candidate},
			recorded:   []RecordedDividend{{AssetID: "asset-2330", TradeDate: day("2026-05-31")}}, // 距 pay_date 46 天
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffMissingDividend, BrokerValue: "5000"},
			},
		},
		{
			name: "沒有 pay_date 時用 ex_date 比對",
			candidates: []DividendCandidate{{
				AssetID: "asset-2330", Symbol: "2330", ExDate: day("2026-03-10"),
				CashPerShare: d("5"), HeldShares: d("1000"),
			}},
			recorded: []RecordedDividend{{AssetID: "asset-2330", TradeDate: day("2026-04-20")}}, // 距 ex_date 41 天
			want:     nil,
		},
		{
			name:       "別的資產的事件不能拿來配對",
			candidates: []DividendCandidate{candidate},
			recorded:   []RecordedDividend{{AssetID: "asset-0050", TradeDate: day("2026-04-15")}},
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffMissingDividend, BrokerValue: "5000"},
			},
		},
		{
			name: "除息前持股為零的股利不算漏記",
			candidates: []DividendCandidate{{
				AssetID: "asset-2330", Symbol: "2330", ExDate: day("2026-03-10"),
				PayDate: dayPtr("2026-04-15"), CashPerShare: d("5"), HeldShares: d("0"),
			}},
			recorded: nil,
			want:     nil,
		},
		{
			name: "兩筆股利只有一筆入帳：事件配給較近的那筆，另一筆列漏記",
			candidates: []DividendCandidate{
				candidate, // pay 2026-04-15
				{
					ActionID: "action-2", AssetID: "asset-2330", Symbol: "2330",
					ExDate: day("2026-06-10"), PayDate: dayPtr("2026-07-15"),
					CashPerShare: d("3"), HeldShares: d("1000"),
				},
			},
			recorded: []RecordedDividend{{AssetID: "asset-2330", TradeDate: day("2026-04-16")}},
			want: []ComputedDiff{
				{AssetID: "asset-2330", Symbol: "2330", DiffType: DiffMissingDividend, BrokerValue: "3000"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeMissingDividends(tc.candidates, tc.recorded)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ComputeMissingDividends() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
