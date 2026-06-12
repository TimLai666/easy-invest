package strategy

import (
	"testing"

	"github.com/shopspring/decimal"
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
