package strategy

import (
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"
)

type Position struct {
	Symbol         string
	AssetID        string
	AssetType      string
	LotSize        int
	QuantityShares decimal.Decimal
	Price          decimal.Decimal
}

type Settings struct {
	TargetWeights  map[string]decimal.Decimal
	RebalanceBand  decimal.Decimal
	CashBuffer     decimal.Decimal
	MinTradeAmount decimal.Decimal
	PreferWholeLot bool
}

type Input struct {
	Positions []Position
	Cash      decimal.Decimal
	Settings  Settings
}

type Intent struct {
	Symbol          string
	AssetID         string
	Action          string
	QuantityShares  decimal.Decimal
	EstimatedPrice  decimal.Decimal
	EstimatedAmount decimal.Decimal
	CurrentWeight   decimal.Decimal
	TargetWeight    decimal.Decimal
	Reason          string
	Risks           string
	Confidence      decimal.Decimal
}

func Rebalance(input Input) []Intent {
	total := input.Cash
	bySymbol := make(map[string]Position, len(input.Positions))
	for _, p := range input.Positions {
		bySymbol[p.Symbol] = p
		total = total.Add(p.QuantityShares.Mul(p.Price))
	}
	if total.LessThanOrEqual(decimal.Zero) {
		return []Intent{{
			Action:       "no_action",
			Reason:       "目前沒有可計算的現金或持股。",
			Risks:        "請先新增交易或匯入市場資料。",
			Confidence:   decimal.RequireFromString("0.5"),
			TargetWeight: decimal.Zero,
		}}
	}

	symbols := make([]string, 0, len(input.Settings.TargetWeights))
	for symbol := range input.Settings.TargetWeights {
		if symbol == "cash" {
			continue
		}
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	var intents []Intent
	for _, symbol := range symbols {
		target := input.Settings.TargetWeights[symbol]
		p, ok := bySymbol[symbol]
		if !ok || p.Price.LessThanOrEqual(decimal.Zero) {
			intents = append(intents, Intent{
				Symbol:       symbol,
				Action:       "no_action",
				TargetWeight: target,
				Reason:       fmt.Sprintf("%s 缺少持股或收盤價，暫不提出交易建議。", symbol),
				Risks:        "資料不足時不估算買賣數量。",
				Confidence:   decimal.RequireFromString("0.4"),
			})
			continue
		}
		currentValue := p.QuantityShares.Mul(p.Price)
		currentWeight := currentValue.Div(total)
		diffWeight := target.Sub(currentWeight)
		if diffWeight.Abs().LessThanOrEqual(input.Settings.RebalanceBand) {
			intents = append(intents, Intent{
				Symbol:         symbol,
				AssetID:        p.AssetID,
				Action:         "hold",
				QuantityShares: decimal.Zero,
				EstimatedPrice: p.Price,
				CurrentWeight:  currentWeight,
				TargetWeight:   target,
				Reason:         fmt.Sprintf("%s 目前權重 %s，仍在目標權重 %s 的再平衡區間內。", symbol, pct(currentWeight), pct(target)),
				Risks:          "使用最新入庫收盤價估算，市價可能不同。",
				Confidence:     decimal.RequireFromString("0.7"),
			})
			continue
		}

		diffAmount := diffWeight.Mul(total)
		action := "buy"
		if diffAmount.IsNegative() {
			action = "sell"
			diffAmount = diffAmount.Abs()
		}
		if diffAmount.LessThan(input.Settings.MinTradeAmount) {
			intents = append(intents, Intent{
				Symbol:          symbol,
				AssetID:         p.AssetID,
				Action:          "no_action",
				QuantityShares:  decimal.Zero,
				EstimatedPrice:  p.Price,
				EstimatedAmount: diffAmount,
				CurrentWeight:   currentWeight,
				TargetWeight:    target,
				Reason:          fmt.Sprintf("%s 雖偏離目標，但估算交易金額 %s 低於最小交易金額。", symbol, diffAmount.StringFixed(0)),
				Risks:           "低金額交易可能被手續費侵蝕。",
				Confidence:      decimal.RequireFromString("0.6"),
			})
			continue
		}

		quantity := diffAmount.Div(p.Price).Floor()
		if input.Settings.PreferWholeLot && p.LotSize > 1 {
			lotSize := decimal.NewFromInt(int64(p.LotSize))
			quantity = quantity.Div(lotSize).Floor().Mul(lotSize)
		}
		if action == "sell" && quantity.GreaterThan(p.QuantityShares) {
			quantity = p.QuantityShares
		}
		if quantity.LessThanOrEqual(decimal.Zero) {
			intents = append(intents, Intent{
				Symbol:        symbol,
				AssetID:       p.AssetID,
				Action:        "no_action",
				CurrentWeight: currentWeight,
				TargetWeight:  target,
				Reason:        fmt.Sprintf("%s 計算後數量低於 1 股，暫不提出交易建議。", symbol),
				Risks:         "偏好整張或低金額可能讓建議數量歸零。",
				Confidence:    decimal.RequireFromString("0.5"),
			})
			continue
		}

		amount := quantity.Mul(p.Price)
		intents = append(intents, Intent{
			Symbol:          symbol,
			AssetID:         p.AssetID,
			Action:          action,
			QuantityShares:  quantity,
			EstimatedPrice:  p.Price,
			EstimatedAmount: amount,
			CurrentWeight:   currentWeight,
			TargetWeight:    target,
			Reason:          fmt.Sprintf("%s 目前權重 %s，目標權重 %s，已超出再平衡區間。", symbol, pct(currentWeight), pct(target)),
			Risks:           "使用最新入庫收盤價估算，實際成交價格與費稅可能不同。",
			Confidence:      decimal.RequireFromString("0.7"),
		})
	}
	if len(intents) == 0 {
		intents = append(intents, Intent{
			Action:     "no_action",
			Reason:     "尚未設定目標權重。",
			Risks:      "請先設定目標權重後再產生建議。",
			Confidence: decimal.RequireFromString("0.5"),
		})
	}
	return intents
}

func pct(d decimal.Decimal) string {
	f, _ := d.Mul(decimal.NewFromInt(100)).Float64()
	return fmt.Sprintf("%.2f%%", math.Round(f*100)/100)
}
