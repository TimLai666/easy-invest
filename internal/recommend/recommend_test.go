package recommend

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// ---------------------------------------------------------------------------
// parseWeights — 權重 JSON 解析
// ---------------------------------------------------------------------------

func TestParseWeights(t *testing.T) {
	tests := []struct {
		name string
		json string
		want map[string]string // symbol → decimal string
	}{
		{
			name: "字串值",
			json: `{"0050":"0.5","00878":"0.3","cash":"0.2"}`,
			want: map[string]string{"0050": "0.5", "00878": "0.3", "cash": "0.2"},
		},
		{
			name: "數字值",
			json: `{"0050":0.5,"00878":0.3}`,
			want: map[string]string{"0050": "0.5", "00878": "0.3"},
		},
		{
			name: "混合字串與數字",
			json: `{"0050":"0.6","00878":0.4}`,
			want: map[string]string{"0050": "0.6", "00878": "0.4"},
		},
		{
			name: "空 JSON",
			json: `{}`,
			want: map[string]string{},
		},
		{
			name: "無效 JSON 回空 map",
			json: `not json`,
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWeights([]byte(tt.json))
			if len(got) != len(tt.want) {
				t.Fatalf("長度 = %d, want %d", len(got), len(tt.want))
			}
			for symbol, wantStr := range tt.want {
				v, ok := got[symbol]
				if !ok {
					t.Fatalf("缺少 symbol %q", symbol)
				}
				if v.String() != wantStr {
					t.Fatalf("got[%q] = %s, want %s", symbol, v.String(), wantStr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 輔助函式
// ---------------------------------------------------------------------------

func TestNullableDecimal(t *testing.T) {
	tests := []struct {
		name   string
		input  decimal.Decimal
		isNil  bool
	}{
		{"零值回 nil", decimal.Zero, true},
		{"非零值回字串", decimal.NewFromInt(42), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullableDecimal(tt.input)
			if tt.isNil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
			} else {
				s, ok := got.(string)
				if !ok {
					t.Fatalf("got type %T, want string", got)
				}
				if s != tt.input.String() {
					t.Fatalf("got %q, want %q", s, tt.input.String())
				}
			}
		})
	}
}

func TestDecimalPtrString(t *testing.T) {
	tests := []struct {
		name  string
		input decimal.Decimal
		isNil bool
	}{
		{"零值回 nil", decimal.Zero, true},
		{"非零值回字串指標", decimal.RequireFromString("3.14"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decimalPtrString(tt.input)
			if tt.isNil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("got nil, want non-nil")
				}
				if *got != tt.input.String() {
					t.Fatalf("got %q, want %q", *got, tt.input.String())
				}
			}
		})
	}
}

func TestStringPtr(t *testing.T) {
	tests := []struct {
		name  string
		input string
		isNil bool
	}{
		{"空字串回 nil", "", true},
		{"非空字串回指標", "hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringPtr(tt.input)
			if tt.isNil && got != nil {
				t.Fatalf("got %v, want nil", got)
			}
			if !tt.isNil {
				if got == nil || *got != tt.input {
					t.Fatalf("got %v, want %q", got, tt.input)
				}
			}
		})
	}
}

func TestStringOrEmpty(t *testing.T) {
	tests := []struct {
		name string
		input any
		want string
	}{
		{"nil 回空字串", nil, ""},
		{"string 回原值", "abc", "abc"},
		{"非 string 回空字串", 123, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringOrEmpty(tt.input)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	if IsNotFound(nil) {
		t.Fatal("nil 不是 not found")
	}
}

// ---------------------------------------------------------------------------
// Disclaimer 常數
// ---------------------------------------------------------------------------

func TestDisclaimerNotEmpty(t *testing.T) {
	if Disclaimer == "" {
		t.Fatal("Disclaimer 不可為空")
	}
	if !strings.Contains(Disclaimer, "不構成投資建議") {
		t.Fatal("Disclaimer 應包含免責聲明關鍵詞")
	}
}

// ---------------------------------------------------------------------------
// Rebalance 策略邊界測試（recommend 呼叫的核心下游）
// 補充 rebalance_test.go 未涵蓋的場景。
// ---------------------------------------------------------------------------

// 空持倉 + 零現金 → no_action。
func TestRebalanceEmptyPortfolioZeroCash(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash:      decimal.Zero,
		Positions: nil,
		Settings: strategy.Settings{
			TargetWeights: map[string]decimal.Decimal{
				"0050": decimal.RequireFromString("0.5"),
			},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	if len(intents) != 1 {
		t.Fatalf("len = %d, want 1", len(intents))
	}
	if intents[0].Action != "no_action" {
		t.Fatalf("action = %s, want no_action", intents[0].Action)
	}
}

// 沒有設定目標權重 → no_action。
func TestRebalanceNoTargetWeights(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash:      decimal.NewFromInt(100000),
		Positions: nil,
		Settings: strategy.Settings{
			TargetWeights:  map[string]decimal.Decimal{},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	if len(intents) != 1 {
		t.Fatalf("len = %d, want 1", len(intents))
	}
	if intents[0].Action != "no_action" {
		t.Fatalf("action = %s, want no_action", intents[0].Action)
	}
}

// 只有現金、沒有持倉但有目標權重 → 對應的 symbol 應報 no_action（缺持股資料）。
func TestRebalanceCashOnlyNoPositions(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(500000),
		Settings: strategy.Settings{
			TargetWeights: map[string]decimal.Decimal{
				"0050":  decimal.RequireFromString("0.6"),
				"00878": decimal.RequireFromString("0.4"),
			},
			RebalanceBand:  decimal.RequireFromString("0.01"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	// 沒有任何 Position 傳入，兩個 symbol 都缺持股，應為 no_action。
	for _, intent := range intents {
		if intent.Action != "no_action" {
			t.Fatalf("symbol %s action = %s, want no_action（缺持股資料）", intent.Symbol, intent.Action)
		}
	}
}

// 持股的 Price 為 0 → 該 symbol 報 no_action。
func TestRebalanceZeroPricePosition(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(100000),
		Positions: []strategy.Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(1000),
			Price:          decimal.Zero, // 沒有市價
		}},
		Settings: strategy.Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.RequireFromString("0.5")},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	found := false
	for _, intent := range intents {
		if intent.Symbol == "0050" {
			found = true
			if intent.Action != "no_action" {
				t.Fatalf("action = %s, want no_action（Price = 0）", intent.Action)
			}
		}
	}
	if !found {
		t.Fatal("結果中應包含 0050")
	}
}

// 單一標的全部賣出：賣出數量不應超過持有股數。
func TestRebalanceSellCappedAtHolding(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(500000),
		Positions: []strategy.Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			AssetType:      twmarket.AssetTWStock,
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(100),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: strategy.Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.RequireFromString("0.01")},
			RebalanceBand:  decimal.RequireFromString("0.001"),
			MinTradeAmount: decimal.NewFromInt(1),
		},
	})
	for _, intent := range intents {
		if intent.Symbol == "0050" && intent.Action == "sell" {
			if intent.QuantityShares.GreaterThan(decimal.NewFromInt(100)) {
				t.Fatalf("賣出數量 %s 超過持有 100 股", intent.QuantityShares)
			}
			return
		}
	}
	// 金額太小可能變 no_action，也是合理結果。
}

// 偏好整張：買進數量應為 lotSize 的整數倍。
func TestRebalancePreferWholeLot(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(200000),
		Positions: []strategy.Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			AssetType:      twmarket.AssetTWETF,
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(0),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: strategy.Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.RequireFromString("0.8")},
			RebalanceBand:  decimal.RequireFromString("0.01"),
			MinTradeAmount: decimal.NewFromInt(1000),
			PreferWholeLot: true,
		},
	})
	for _, intent := range intents {
		if intent.Symbol == "0050" && intent.Action == "buy" {
			rem := intent.QuantityShares.Mod(decimal.NewFromInt(1000))
			if !rem.IsZero() {
				t.Fatalf("偏好整張時買進數量 %s 不是 1000 的整數倍", intent.QuantityShares)
			}
			return
		}
	}
}

// Sequence 應從 1 開始連續編號。
func TestRebalanceSequenceStartsAt1(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(100000),
		Positions: []strategy.Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(1000),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: strategy.Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.RequireFromString("0.5")},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	for i, intent := range intents {
		if intent.Sequence != i+1 {
			t.Fatalf("intents[%d].Sequence = %d, want %d", i, intent.Sequence, i+1)
		}
	}
}

// 多標的排序：賣出在前、買進在後。
func TestRebalanceSellsBeforeBuys(t *testing.T) {
	intents := strategy.Rebalance(strategy.Input{
		Cash: decimal.NewFromInt(100000),
		Positions: []strategy.Position{
			{
				Symbol: "2330", AssetID: "a-2330", AssetType: twmarket.AssetTWStock,
				LotSize: 1000, QuantityShares: decimal.NewFromInt(2000),
				Price: decimal.NewFromInt(100),
			},
			{
				Symbol: "0050", AssetID: "a-0050", AssetType: twmarket.AssetTWETF,
				LotSize: 1000, QuantityShares: decimal.NewFromInt(0),
				Price: decimal.NewFromInt(50),
			},
		},
		Settings: strategy.Settings{
			TargetWeights: map[string]decimal.Decimal{
				"2330": decimal.RequireFromString("0.3"),
				"0050": decimal.RequireFromString("0.5"),
			},
			RebalanceBand:  decimal.RequireFromString("0.01"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	seenBuy := false
	for _, intent := range intents {
		if intent.Action == "buy" {
			seenBuy = true
		}
		if intent.Action == "sell" && seenBuy {
			t.Fatal("賣出應排在買進之前")
		}
	}
}

// NormalizeFees 零值欄位補台股預設值。
func TestNormalizeFees(t *testing.T) {
	fees := strategy.NormalizeFees(twmarket.FeeSettings{})
	if fees.FeeRate.IsZero() {
		t.Fatal("FeeRate 不應為零")
	}
	if fees.FeeDiscount.IsZero() {
		t.Fatal("FeeDiscount 不應為零")
	}
	if fees.FeeMinimum.IsZero() {
		t.Fatal("FeeMinimum 不應為零")
	}
	if fees.DividendTransferFee.IsZero() {
		t.Fatal("DividendTransferFee 不應為零")
	}
}

// NormalizeFees 非零值不覆蓋。
func TestNormalizeFeesKeepsExisting(t *testing.T) {
	custom := twmarket.FeeSettings{
		FeeRate:             decimal.RequireFromString("0.001"),
		FeeDiscount:         decimal.RequireFromString("0.5"),
		FeeMinimum:          decimal.NewFromInt(1),
		DividendTransferFee: decimal.NewFromInt(5),
	}
	fees := strategy.NormalizeFees(custom)
	if !fees.FeeRate.Equal(custom.FeeRate) {
		t.Fatalf("FeeRate = %s, want %s", fees.FeeRate, custom.FeeRate)
	}
	if !fees.FeeDiscount.Equal(custom.FeeDiscount) {
		t.Fatalf("FeeDiscount = %s, want %s", fees.FeeDiscount, custom.FeeDiscount)
	}
	if !fees.FeeMinimum.Equal(custom.FeeMinimum) {
		t.Fatalf("FeeMinimum = %s, want %s", fees.FeeMinimum, custom.FeeMinimum)
	}
	if !fees.DividendTransferFee.Equal(custom.DividendTransferFee) {
		t.Fatalf("DividendTransferFee = %s, want %s", fees.DividendTransferFee, custom.DividendTransferFee)
	}
}
