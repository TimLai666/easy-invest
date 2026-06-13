// Package strategy 是純計算的策略核心：不做任何網路、資料庫或檔案 I/O，
// 輸入由外層（recommend、backtest）整理好之後交給本套件計算。
package strategy

import (
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/twmarket"
)

// Position 是策略輸入中的單一持股狀態，數量一律以「股」為單位。
type Position struct {
	Symbol         string
	AssetID        string
	AssetType      string
	LotSize        int
	QuantityShares decimal.Decimal
	Price          decimal.Decimal
}

// Settings 是目標權重再平衡策略的參數。
type Settings struct {
	TargetWeights  map[string]decimal.Decimal
	RebalanceBand  decimal.Decimal
	CashBuffer     decimal.Decimal
	MinTradeAmount decimal.Decimal
	PreferWholeLot bool
	// Fees 用於估算賣出淨入帳金額，作為買進現金預算的一部分。
	// 零值欄位會以台股預設費率補齊（見 NormalizeFees）。
	Fees twmarket.FeeSettings
}

// Input 是策略核心的完整輸入。
type Input struct {
	Positions []Position
	Cash      decimal.Decimal
	Settings  Settings
}

// Intent 是單一標的的交易建議。
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
	// Sequence 是建議的執行順序（從 1 開始）：先賣後買，
	// 依序執行時賣出入帳的現金才足以支應後續買進。
	Sequence int
}

// Rebalance 依目標權重產生交易建議。輸出排序固定為：先賣出、再買進、
// 最後是持有與不行動，並以 Sequence 標明執行順序。
//
// 現金規則：
//   - 可動用現金 = 現金 − 現金緩衝（低於 0 視為 0）。
//   - 買進總額不得超過「可動用現金 + 賣出淨入帳估算」；
//     不足時按權重缺口比例縮減買單，縮減後低於最小交易金額的買單剔除。
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
			Sequence:     1,
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

	fees := NormalizeFees(input.Settings.Fees)

	var sells, buys, others []Intent
	sellNetTotal := decimal.Zero
	buyTotal := decimal.Zero
	for _, symbol := range symbols {
		target := input.Settings.TargetWeights[symbol]
		p, ok := bySymbol[symbol]
		if !ok || p.Price.LessThanOrEqual(decimal.Zero) {
			others = append(others, Intent{
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
			others = append(others, Intent{
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
			others = append(others, Intent{
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

		quantity := floorQuantity(diffAmount, p.Price, p.LotSize, input.Settings.PreferWholeLot)
		if action == "sell" && quantity.GreaterThan(p.QuantityShares) {
			quantity = p.QuantityShares
		}
		if quantity.LessThanOrEqual(decimal.Zero) {
			others = append(others, Intent{
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
		intent := Intent{
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
		}
		if action == "sell" {
			sellNetTotal = sellNetTotal.Add(sellNetProceeds(amount, p.AssetType, fees))
			sells = append(sells, intent)
		} else {
			buyTotal = buyTotal.Add(amount)
			buys = append(buys, intent)
		}
	}

	// 現金預算：可動用現金 = 現金 − 現金緩衝（低於 0 視為 0），
	// 加上賣出後預估的淨入帳金額（已扣手續費與證交稅）。
	usableCash := input.Cash.Sub(input.Settings.CashBuffer)
	if usableCash.IsNegative() {
		usableCash = decimal.Zero
	}
	budget := usableCash.Add(sellNetTotal)

	if len(buys) > 0 && buyTotal.GreaterThan(budget) {
		if budget.LessThanOrEqual(decimal.Zero) {
			// 完全買不動：把買單改成持有，說清楚原因。
			for _, b := range buys {
				others = append(others, Intent{
					Symbol:         b.Symbol,
					AssetID:        b.AssetID,
					Action:         "hold",
					QuantityShares: decimal.Zero,
					EstimatedPrice: b.EstimatedPrice,
					CurrentWeight:  b.CurrentWeight,
					TargetWeight:   b.TargetWeight,
					Reason:         fmt.Sprintf("%s 低於目標權重，但現金 %s 扣除保留緩衝 %s 後沒有可動用資金，先持有不動。", b.Symbol, input.Cash.StringFixed(0), input.Settings.CashBuffer.StringFixed(0)),
					Risks:          "增加現金或調降現金緩衝後，才能執行買進。",
					Confidence:     decimal.RequireFromString("0.6"),
				})
			}
			buys = nil
		} else {
			scaled := make([]Intent, 0, len(buys))
			for _, b := range buys {
				p := bySymbol[b.Symbol]
				// 各買單依原始金額（即權重缺口）等比例分配預算。
				scaledAmount := b.EstimatedAmount.Mul(budget).Div(buyTotal)
				quantity := floorQuantity(scaledAmount, p.Price, p.LotSize, input.Settings.PreferWholeLot)
				amount := quantity.Mul(p.Price)
				if quantity.LessThanOrEqual(decimal.Zero) || amount.LessThan(input.Settings.MinTradeAmount) {
					others = append(others, Intent{
						Symbol:         b.Symbol,
						AssetID:        b.AssetID,
						Action:         "no_action",
						QuantityShares: decimal.Zero,
						EstimatedPrice: b.EstimatedPrice,
						CurrentWeight:  b.CurrentWeight,
						TargetWeight:   b.TargetWeight,
						Reason:         fmt.Sprintf("%s 原建議買進 %s 元，但可動用現金不足，按權重缺口比例縮減後剩 %s 元，低於最小交易金額，本次剔除。", b.Symbol, b.EstimatedAmount.StringFixed(0), amount.StringFixed(0)),
						Risks:          "可動用現金 = 現金 − 現金緩衝 + 賣出淨入帳估算。",
						Confidence:     decimal.RequireFromString("0.6"),
					})
					continue
				}
				b.QuantityShares = quantity
				b.EstimatedAmount = amount
				b.Reason += fmt.Sprintf("可動用現金有限，買進金額已按權重缺口比例縮減為 %s 元。", amount.StringFixed(0))
				b.Risks += "估算不含買進手續費，實際所需現金略高。"
				scaled = append(scaled, b)
			}
			buys = scaled
		}
	}

	intents := make([]Intent, 0, len(sells)+len(buys)+len(others))
	intents = append(intents, sells...)
	intents = append(intents, buys...)
	intents = append(intents, others...)
	if len(intents) == 0 {
		intents = append(intents, Intent{
			Action:     "no_action",
			Reason:     "尚未設定目標權重。",
			Risks:      "請先設定目標權重後再產生建議。",
			Confidence: decimal.RequireFromString("0.5"),
		})
	}
	for i := range intents {
		intents[i].Sequence = i + 1
	}
	return intents
}

// NormalizeFees 將零值費率欄位補上台股預設值，讓估算一律有保守費率可用。
func NormalizeFees(f twmarket.FeeSettings) twmarket.FeeSettings {
	if f.FeeRate.IsZero() {
		f.FeeRate = twmarket.DefaultFeeRate
	}
	if f.FeeDiscount.IsZero() {
		f.FeeDiscount = twmarket.DefaultFeeDiscount
	}
	if f.FeeMinimum.IsZero() {
		f.FeeMinimum = twmarket.DefaultFeeMinimum
	}
	if f.DividendTransferFee.IsZero() {
		f.DividendTransferFee = twmarket.DefaultDividendFee
	}
	return f
}

// sellNetProceeds 估算賣出後實際入帳金額：成交金額扣除手續費與證交稅。
func sellNetProceeds(gross decimal.Decimal, assetType string, fees twmarket.FeeSettings) decimal.Decimal {
	net := gross.Sub(twmarket.EstimateFee(gross, fees)).Sub(twmarket.EstimateSecuritiesTax(gross, assetType))
	if net.IsNegative() {
		return decimal.Zero
	}
	return net
}

// floorQuantity 把金額換算成股數並無條件捨去；偏好整張時再捨到整張。
func floorQuantity(amount, price decimal.Decimal, lotSize int, preferWholeLot bool) decimal.Decimal {
	quantity := amount.Div(price).Floor()
	if preferWholeLot && lotSize > 1 {
		ls := decimal.NewFromInt(int64(lotSize))
		quantity = quantity.Div(ls).Floor().Mul(ls)
	}
	return quantity
}

func pct(d decimal.Decimal) string {
	f, _ := d.Mul(decimal.NewFromInt(100)).Float64()
	return fmt.Sprintf("%.2f%%", math.Round(f*100)/100)
}
