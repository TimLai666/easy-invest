// Package backtest 提供回測核心與基準計算。核心是純函式：
// 重用 strategy.Rebalance 在每月第一個交易日計算交易、以當日收盤價成交
// （保守假設），費稅一律用 twmarket 公式；不做任何網路、資料庫或檔案 I/O。
// 資料載入與結果保存由 Service（service.go）負責。
package backtest

import (
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// Bar 是單一交易日的日 K 摘要；回測只使用收盤價。
type Bar struct {
	Date  time.Time
	Close decimal.Decimal
}

// AssetMeta 是回測需要的標的基本資料。
type AssetMeta struct {
	AssetID   string
	AssetType string // twmarket 資產類別，決定證交稅率
	LotSize   int    // 一張的股數；0 視為台股預設 1000
}

// Input 是回測核心的完整輸入。
type Input struct {
	Bars        map[string][]Bar     // symbol → 日 K（日期昇冪；未排序也會先排序）
	Assets      map[string]AssetMeta // symbol → 標的資料
	InitialCash decimal.Decimal
	Settings    strategy.Settings
	Fees        twmarket.FeeSettings // 零值欄位以台股預設補齊，並覆寫 Settings.Fees
}

// Trade 是回測過程中的一筆模擬成交。
type Trade struct {
	Date           time.Time
	Symbol         string
	Action         string // buy 或 sell
	QuantityShares decimal.Decimal
	Price          decimal.Decimal
	GrossAmount    decimal.Decimal
	Fee            decimal.Decimal
	Tax            decimal.Decimal
	CashDelta      decimal.Decimal // 對現金的影響：買進為負、賣出為正
}

// EquityPoint 是權益曲線上的一點（現金 + 持股市值）。
type EquityPoint struct {
	Date   time.Time
	Equity decimal.Decimal
}

// RebalanceRecord 保留單一再平衡日的策略輸入與輸出，供共用核心驗證與回放。
type RebalanceRecord struct {
	Date    time.Time
	Input   strategy.Input
	Intents []strategy.Intent
}

// Result 是回測輸出；策略與基準共用同一格式。
type Result struct {
	InitialEquity    decimal.Decimal
	FinalEquity      decimal.Decimal
	TotalReturn      decimal.Decimal // 期末 / 期初 − 1
	AnnualizedReturn decimal.Decimal // 以 365 天年化
	MaxDrawdown      decimal.Decimal // 正值幅度，0.1 代表回撤 10%
	EquityCurve      []EquityPoint
	Trades           []Trade
	FinalPositions   map[string]decimal.Decimal // symbol → 股數
	FinalCash        decimal.Decimal
	// Rebalances 依時間排序；最後一筆即最後一個再平衡日的建議，
	// 用來驗證回測與線上引擎共用同一套策略核心。
	Rebalances []RebalanceRecord
}

// Simulate 執行再平衡策略回測：每月第一個交易日重用 strategy.Rebalance 計算
// 交易，依建議的 Sequence（先賣後買）以當日收盤價成交。
func Simulate(input Input) (Result, error) {
	if len(input.Bars) == 0 {
		return Result{}, errors.New("回測需要至少一檔標的的歷史行情")
	}
	if input.InitialCash.LessThanOrEqual(decimal.Zero) {
		return Result{}, errors.New("期初資金必須大於 0")
	}
	days := tradingDays(input.Bars)
	if len(days) == 0 {
		return Result{}, errors.New("歷史行情中沒有任何交易日")
	}
	closes := closeIndex(input.Bars)
	rebalanceDays := monthlyFirstTradingDaySet(days)

	fees := strategy.NormalizeFees(input.Fees)
	settings := input.Settings
	settings.Fees = fees

	cash := input.InitialCash
	positions := map[string]decimal.Decimal{}
	lastClose := map[string]decimal.Decimal{}

	result := Result{InitialEquity: input.InitialCash}
	for _, day := range days {
		key := dateKey(day)
		for symbol := range input.Bars {
			if c, ok := closes[symbol][key]; ok {
				lastClose[symbol] = c
			}
		}
		if rebalanceDays[key] {
			stratInput := buildStrategyInput(cash, positions, lastClose, input.Assets, settings)
			intents := strategy.Rebalance(stratInput)
			result.Rebalances = append(result.Rebalances, RebalanceRecord{Date: day, Input: stratInput, Intents: intents})
			// 依策略輸出順序執行：先賣後買，現金才足夠。
			for _, intent := range intents {
				switch intent.Action {
				case "sell":
					if trade, ok := executeSell(day, intent, positions, &cash, input.Assets, fees); ok {
						result.Trades = append(result.Trades, trade)
					}
				case "buy":
					if trade, ok := executeBuy(day, intent, positions, &cash, input.Assets, fees, settings.PreferWholeLot); ok {
						result.Trades = append(result.Trades, trade)
					}
				}
			}
		}
		result.EquityCurve = append(result.EquityCurve, EquityPoint{Date: day, Equity: equityOf(cash, positions, lastClose)})
	}

	result.FinalCash = cash
	result.FinalPositions = nonZeroPositions(positions)
	result.FinalEquity = result.EquityCurve[len(result.EquityCurve)-1].Equity
	fillMetrics(&result)
	return result, nil
}

// buildStrategyInput 把回測當下狀態整理成策略輸入：
// 涵蓋「目前持有」與「目標權重提及」的所有標的，缺價時留零值讓策略自行說明資料不足。
func buildStrategyInput(cash decimal.Decimal, positions, lastClose map[string]decimal.Decimal, assets map[string]AssetMeta, settings strategy.Settings) strategy.Input {
	symbolSet := map[string]bool{}
	for symbol, qty := range positions {
		if qty.GreaterThan(decimal.Zero) {
			symbolSet[symbol] = true
		}
	}
	for symbol := range settings.TargetWeights {
		if symbol != "cash" {
			symbolSet[symbol] = true
		}
	}
	symbols := make([]string, 0, len(symbolSet))
	for symbol := range symbolSet {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	stratPositions := make([]strategy.Position, 0, len(symbols))
	for _, symbol := range symbols {
		meta := assets[symbol]
		stratPositions = append(stratPositions, strategy.Position{
			Symbol:         symbol,
			AssetID:        meta.AssetID,
			AssetType:      meta.AssetType,
			LotSize:        lotSizeOrDefault(meta),
			QuantityShares: positions[symbol],
			Price:          lastClose[symbol],
		})
	}
	return strategy.Input{Cash: cash, Positions: stratPositions, Settings: settings}
}

// executeSell 以收盤價賣出，入帳金額 = 成交金額 − 手續費 − 證交稅。
func executeSell(day time.Time, intent strategy.Intent, positions map[string]decimal.Decimal, cash *decimal.Decimal, assets map[string]AssetMeta, fees twmarket.FeeSettings) (Trade, bool) {
	held := positions[intent.Symbol]
	qty := intent.QuantityShares
	if qty.GreaterThan(held) {
		qty = held
	}
	if qty.LessThanOrEqual(decimal.Zero) || intent.EstimatedPrice.LessThanOrEqual(decimal.Zero) {
		return Trade{}, false
	}
	gross := qty.Mul(intent.EstimatedPrice)
	fee := twmarket.EstimateFee(gross, fees)
	tax := twmarket.EstimateSecuritiesTax(gross, assets[intent.Symbol].AssetType)
	net := gross.Sub(fee).Sub(tax)
	*cash = cash.Add(net)
	positions[intent.Symbol] = held.Sub(qty)
	return Trade{
		Date: day, Symbol: intent.Symbol, Action: "sell",
		QuantityShares: qty, Price: intent.EstimatedPrice,
		GrossAmount: gross, Fee: fee, Tax: tax, CashDelta: net,
	}, true
}

// executeBuy 以收盤價買進，支出 = 成交金額 + 手續費。
// 策略的預算估算不含買進手續費，現金不足時逐步調降股數（保守處理）。
func executeBuy(day time.Time, intent strategy.Intent, positions map[string]decimal.Decimal, cash *decimal.Decimal, assets map[string]AssetMeta, fees twmarket.FeeSettings, preferWholeLot bool) (Trade, bool) {
	qty := intent.QuantityShares
	price := intent.EstimatedPrice
	if qty.LessThanOrEqual(decimal.Zero) || price.LessThanOrEqual(decimal.Zero) {
		return Trade{}, false
	}
	step := decimal.NewFromInt(1)
	if meta := assets[intent.Symbol]; preferWholeLot && lotSizeOrDefault(meta) > 1 {
		step = decimal.NewFromInt(int64(lotSizeOrDefault(meta)))
	}
	for qty.GreaterThan(decimal.Zero) {
		gross := qty.Mul(price)
		fee := twmarket.EstimateFee(gross, fees)
		cost := gross.Add(fee)
		if cost.LessThanOrEqual(*cash) {
			*cash = cash.Sub(cost)
			positions[intent.Symbol] = positions[intent.Symbol].Add(qty)
			return Trade{
				Date: day, Symbol: intent.Symbol, Action: "buy",
				QuantityShares: qty, Price: price,
				GrossAmount: gross, Fee: fee, Tax: decimal.Zero, CashDelta: cost.Neg(),
			}, true
		}
		qty = qty.Sub(step)
	}
	return Trade{}, false
}

func equityOf(cash decimal.Decimal, positions, lastClose map[string]decimal.Decimal) decimal.Decimal {
	equity := cash
	for symbol, qty := range positions {
		if qty.GreaterThan(decimal.Zero) {
			equity = equity.Add(qty.Mul(lastClose[symbol]))
		}
	}
	return equity
}

func nonZeroPositions(positions map[string]decimal.Decimal) map[string]decimal.Decimal {
	final := map[string]decimal.Decimal{}
	for symbol, qty := range positions {
		if qty.GreaterThan(decimal.Zero) {
			final[symbol] = qty
		}
	}
	return final
}

// fillMetrics 計算總報酬、年化報酬與最大回撤。
func fillMetrics(result *Result) {
	if len(result.EquityCurve) == 0 || result.InitialEquity.LessThanOrEqual(decimal.Zero) {
		return
	}
	one := decimal.NewFromInt(1)
	growth := result.FinalEquity.Div(result.InitialEquity)
	result.TotalReturn = growth.Sub(one)

	first := result.EquityCurve[0].Date
	last := result.EquityCurve[len(result.EquityCurve)-1].Date
	days := int64(last.Sub(first) / (24 * time.Hour))
	if days <= 0 || growth.LessThanOrEqual(decimal.Zero) {
		result.AnnualizedReturn = result.TotalReturn
	} else {
		exponent := decimal.NewFromInt(365).Div(decimal.NewFromInt(days))
		if annual, err := growth.PowWithPrecision(exponent, 12); err == nil {
			result.AnnualizedReturn = annual.Sub(one)
		} else {
			// 無法計算冪次時退回總報酬，避免在純函式內 panic。
			result.AnnualizedReturn = result.TotalReturn
		}
	}

	peak := decimal.Zero
	maxDrawdown := decimal.Zero
	for _, point := range result.EquityCurve {
		if point.Equity.GreaterThan(peak) {
			peak = point.Equity
		}
		if peak.GreaterThan(decimal.Zero) {
			drawdown := peak.Sub(point.Equity).Div(peak)
			if drawdown.GreaterThan(maxDrawdown) {
				maxDrawdown = drawdown
			}
		}
	}
	result.MaxDrawdown = maxDrawdown
}

// tradingDays 取所有標的日 K 日期的聯集（去重、昇冪、固定 UTC 零點）。
func tradingDays(bars map[string][]Bar) []time.Time {
	seen := map[string]time.Time{}
	for _, series := range bars {
		for _, bar := range series {
			day := normalizeDate(bar.Date)
			seen[dateKey(day)] = day
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	days := make([]time.Time, 0, len(keys))
	for _, key := range keys {
		days = append(days, seen[key])
	}
	return days
}

// monthlyFirstTradingDaySet 回傳「每月第一個交易日」集合（key 為日期字串）。
func monthlyFirstTradingDaySet(days []time.Time) map[string]bool {
	set := map[string]bool{}
	lastMonth := ""
	for _, day := range days {
		month := day.Format("2006-01")
		if month != lastMonth {
			set[dateKey(day)] = true
			lastMonth = month
		}
	}
	return set
}

func closeIndex(bars map[string][]Bar) map[string]map[string]decimal.Decimal {
	index := make(map[string]map[string]decimal.Decimal, len(bars))
	for symbol, series := range bars {
		byDate := make(map[string]decimal.Decimal, len(series))
		for _, bar := range series {
			byDate[dateKey(normalizeDate(bar.Date))] = bar.Close
		}
		index[symbol] = byDate
	}
	return index
}

func lotSizeOrDefault(meta AssetMeta) int {
	if meta.LotSize <= 0 {
		return 1000
	}
	return meta.LotSize
}

func normalizeDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func dateKey(t time.Time) string {
	return t.Format("2006-01-02")
}
