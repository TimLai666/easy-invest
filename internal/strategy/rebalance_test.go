package strategy

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/twmarket"
)

func TestRebalanceHoldInsideBand(t *testing.T) {
	intents := Rebalance(Input{
		Cash: decimal.NewFromInt(0),
		Positions: []Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(1000),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.RequireFromString("0.96")},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			MinTradeAmount: decimal.NewFromInt(10000),
		},
	})
	if len(intents) != 1 {
		t.Fatalf("len = %d", len(intents))
	}
	if intents[0].Action != "hold" {
		t.Fatalf("action = %s, want hold", intents[0].Action)
	}
}

func TestRebalanceBuyUnderweight(t *testing.T) {
	intents := Rebalance(Input{
		Cash: decimal.NewFromInt(100000),
		Positions: []Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(1000),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.RequireFromString("0.75")},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			MinTradeAmount: decimal.NewFromInt(10000),
		},
	})
	if got, want := intents[0].Action, "buy"; got != want {
		t.Fatalf("action = %s, want %s", got, want)
	}
	if got, want := intents[0].QuantityShares.String(), "500"; got != want {
		t.Fatalf("quantity = %s, want %s", got, want)
	}
}

// 現金緩衝生效：可動用現金 = 現金 − 緩衝，買單按缺口比例縮減。
func TestRebalanceCashBufferScalesBuy(t *testing.T) {
	intents := Rebalance(Input{
		Cash: decimal.NewFromInt(100000),
		Positions: []Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			AssetType:      twmarket.AssetTWETF,
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(1000),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.NewFromInt(1)},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			CashBuffer:     decimal.NewFromInt(60000),
			MinTradeAmount: decimal.NewFromInt(10000),
		},
	})
	if len(intents) != 1 {
		t.Fatalf("len = %d", len(intents))
	}
	got := intents[0]
	if got.Action != "buy" {
		t.Fatalf("action = %s, want buy", got.Action)
	}
	// 原始買進金額 100000，可動用現金 100000-60000=40000，縮減後 400 股。
	if want := "400"; got.QuantityShares.String() != want {
		t.Fatalf("quantity = %s, want %s", got.QuantityShares, want)
	}
	if want := decimal.NewFromInt(40000); !got.EstimatedAmount.Equal(want) {
		t.Fatalf("amount = %s, want %s", got.EstimatedAmount, want)
	}
	if !strings.Contains(got.Reason, "縮減") {
		t.Fatalf("reason 應說明縮減原因，got %q", got.Reason)
	}
}

// 緩衝吃光現金且沒有賣單時，買單應轉為 hold 並附上人話理由。
func TestRebalanceCashBufferBlocksAllBuys(t *testing.T) {
	intents := Rebalance(Input{
		Cash: decimal.NewFromInt(50000),
		Positions: []Position{{
			Symbol:         "0050",
			AssetID:        "asset-0050",
			AssetType:      twmarket.AssetTWETF,
			LotSize:        1000,
			QuantityShares: decimal.NewFromInt(1000),
			Price:          decimal.NewFromInt(100),
		}},
		Settings: Settings{
			TargetWeights:  map[string]decimal.Decimal{"0050": decimal.NewFromInt(1)},
			RebalanceBand:  decimal.RequireFromString("0.05"),
			CashBuffer:     decimal.NewFromInt(50000),
			MinTradeAmount: decimal.NewFromInt(10000),
		},
	})
	if len(intents) != 1 {
		t.Fatalf("len = %d", len(intents))
	}
	got := intents[0]
	if got.Action != "hold" {
		t.Fatalf("action = %s, want hold", got.Action)
	}
	if !got.QuantityShares.IsZero() {
		t.Fatalf("quantity = %s, want 0", got.QuantityShares)
	}
	if !strings.Contains(got.Reason, "緩衝") || !strings.Contains(got.Reason, "持有") {
		t.Fatalf("reason 應為人話說明緩衝後買不動，got %q", got.Reason)
	}
}

// 先賣後買，且買進預算包含賣出淨入帳估算。
func TestRebalanceSellBeforeBuyAndBudgetIncludesSellNet(t *testing.T) {
	intents := Rebalance(Input{
		Cash: decimal.Zero,
		Positions: []Position{
			{
				Symbol:         "0050",
				AssetID:        "asset-0050",
				AssetType:      twmarket.AssetTWETF,
				LotSize:        1000,
				QuantityShares: decimal.NewFromInt(2000),
				Price:          decimal.NewFromInt(100),
			},
			{
				Symbol:         "00878",
				AssetID:        "asset-00878",
				AssetType:      twmarket.AssetTWETF,
				LotSize:        1000,
				QuantityShares: decimal.Zero,
				Price:          decimal.NewFromInt(20),
			},
		},
		Settings: Settings{
			TargetWeights: map[string]decimal.Decimal{
				"0050":  decimal.RequireFromString("0.5"),
				"00878": decimal.RequireFromString("0.5"),
			},
			RebalanceBand:  decimal.RequireFromString("0.01"),
			MinTradeAmount: decimal.NewFromInt(1000),
		},
	})
	if len(intents) != 2 {
		t.Fatalf("len = %d", len(intents))
	}
	sell, buy := intents[0], intents[1]
	if sell.Action != "sell" || sell.Symbol != "0050" {
		t.Fatalf("第一筆應為賣出 0050，got %s %s", sell.Action, sell.Symbol)
	}
	if buy.Action != "buy" || buy.Symbol != "00878" {
		t.Fatalf("第二筆應為買進 00878，got %s %s", buy.Action, buy.Symbol)
	}
	if sell.Sequence != 1 || buy.Sequence != 2 {
		t.Fatalf("sequence = %d, %d，want 1, 2", sell.Sequence, buy.Sequence)
	}
	if want := "1000"; sell.QuantityShares.String() != want {
		t.Fatalf("sell quantity = %s, want %s", sell.QuantityShares, want)
	}
	// 賣出 100000，手續費 142、證交稅 100，淨入帳 99758；
	// 買進 100000 超出預算，縮減後 floor(99758/20)=4987 股。
	if want := "4987"; buy.QuantityShares.String() != want {
		t.Fatalf("buy quantity = %s, want %s", buy.QuantityShares, want)
	}
	if want := decimal.NewFromInt(99740); !buy.EstimatedAmount.Equal(want) {
		t.Fatalf("buy amount = %s, want %s", buy.EstimatedAmount, want)
	}
}

// 縮減後低於最小交易金額的買單應剔除，並在 reason 說明。
func TestRebalanceScaledBuyBelowMinDropped(t *testing.T) {
	intents := Rebalance(Input{
		Cash: decimal.NewFromInt(100000),
		Positions: []Position{
			{
				Symbol:         "0050",
				AssetID:        "asset-0050",
				AssetType:      twmarket.AssetTWETF,
				LotSize:        1000,
				QuantityShares: decimal.NewFromInt(100),
				Price:          decimal.NewFromInt(100),
			},
			{
				Symbol:         "00878",
				AssetID:        "asset-00878",
				AssetType:      twmarket.AssetTWETF,
				LotSize:        1000,
				QuantityShares: decimal.NewFromInt(500),
				Price:          decimal.NewFromInt(20),
			},
		},
		Settings: Settings{
			TargetWeights: map[string]decimal.Decimal{
				"0050":  decimal.RequireFromString("0.5"),
				"00878": decimal.RequireFromString("0.1834"),
			},
			RebalanceBand:  decimal.RequireFromString("0.001"),
			CashBuffer:     decimal.NewFromInt(69000),
			MinTradeAmount: decimal.NewFromInt(10000),
		},
	})
	if len(intents) != 2 {
		t.Fatalf("len = %d", len(intents))
	}
	// 可動用現金 31000，原始買單 0050=50000、00878=12000，
	// 等比例縮減後 0050=25000（250 股）、00878=6000 低於最小交易金額被剔除。
	buy := intents[0]
	if buy.Action != "buy" || buy.Symbol != "0050" {
		t.Fatalf("第一筆應為買進 0050，got %s %s", buy.Action, buy.Symbol)
	}
	if want := "250"; buy.QuantityShares.String() != want {
		t.Fatalf("buy quantity = %s, want %s", buy.QuantityShares, want)
	}
	dropped := intents[1]
	if dropped.Action != "no_action" || dropped.Symbol != "00878" {
		t.Fatalf("第二筆應為剔除的 00878，got %s %s", dropped.Action, dropped.Symbol)
	}
	if !strings.Contains(dropped.Reason, "縮減") || !strings.Contains(dropped.Reason, "最小交易金額") {
		t.Fatalf("reason 應說明縮減後剔除，got %q", dropped.Reason)
	}
}
