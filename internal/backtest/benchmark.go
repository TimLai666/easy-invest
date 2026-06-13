package backtest

import (
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// BenchmarkInput 是基準回測的輸入；輸出與策略回測共用 Result 格式。
type BenchmarkInput struct {
	Symbol        string
	Asset         AssetMeta
	Bars          []Bar
	InitialCash   decimal.Decimal
	MonthlyAmount decimal.Decimal // DCA 每月投入金額（含手續費的總支出上限）
	Fees          twmarket.FeeSettings
}

// BuyAndHold 是「期初一次買進並持有到期末」基準：
// 第一個交易日用期初資金盡量買進（成交金額 + 手續費不超過資金），之後不再交易。
func BuyAndHold(input BenchmarkInput) (Result, error) {
	bars, err := sortedBars(input)
	if err != nil {
		return Result{}, err
	}
	fees := strategy.NormalizeFees(input.Fees)

	cash := input.InitialCash
	shares := decimal.Zero
	result := Result{InitialEquity: input.InitialCash}
	for i, bar := range bars {
		if i == 0 {
			if trade, ok := buyWithin(bar.Date, input.Symbol, cash, bar.Close, fees); ok {
				cash = cash.Add(trade.CashDelta)
				shares = shares.Add(trade.QuantityShares)
				result.Trades = append(result.Trades, trade)
			}
		}
		result.EquityCurve = append(result.EquityCurve, EquityPoint{Date: bar.Date, Equity: cash.Add(shares.Mul(bar.Close))})
	}
	finishBenchmark(&result, input.Symbol, cash, shares)
	return result, nil
}

// DCA 是「定期定額買進」基準：每月第一個交易日以固定金額買進，
// 成交金額 + 手續費控制在每月金額內；現金不足時以剩餘現金為上限。
func DCA(input BenchmarkInput) (Result, error) {
	bars, err := sortedBars(input)
	if err != nil {
		return Result{}, err
	}
	if input.MonthlyAmount.LessThanOrEqual(decimal.Zero) {
		return Result{}, errors.New("定期定額基準需要每月投入金額")
	}
	fees := strategy.NormalizeFees(input.Fees)

	cash := input.InitialCash
	shares := decimal.Zero
	lastMonth := ""
	result := Result{InitialEquity: input.InitialCash}
	for _, bar := range bars {
		month := bar.Date.Format("2006-01")
		if month != lastMonth {
			lastMonth = month
			budget := input.MonthlyAmount
			if budget.GreaterThan(cash) {
				budget = cash
			}
			if trade, ok := buyWithin(bar.Date, input.Symbol, budget, bar.Close, fees); ok {
				cash = cash.Add(trade.CashDelta)
				shares = shares.Add(trade.QuantityShares)
				result.Trades = append(result.Trades, trade)
			}
		}
		result.EquityCurve = append(result.EquityCurve, EquityPoint{Date: bar.Date, Equity: cash.Add(shares.Mul(bar.Close))})
	}
	finishBenchmark(&result, input.Symbol, cash, shares)
	return result, nil
}

// buyWithin 在 budget 內（成交金額 + 手續費）買進最多股數。
func buyWithin(day time.Time, symbol string, budget, price decimal.Decimal, fees twmarket.FeeSettings) (Trade, bool) {
	if budget.LessThanOrEqual(decimal.Zero) || price.LessThanOrEqual(decimal.Zero) {
		return Trade{}, false
	}
	qty := budget.Div(price).Floor()
	for qty.GreaterThan(decimal.Zero) {
		gross := qty.Mul(price)
		fee := twmarket.EstimateFee(gross, fees)
		cost := gross.Add(fee)
		if cost.LessThanOrEqual(budget) {
			return Trade{
				Date: day, Symbol: symbol, Action: "buy",
				QuantityShares: qty, Price: price,
				GrossAmount: gross, Fee: fee, Tax: decimal.Zero, CashDelta: cost.Neg(),
			}, true
		}
		qty = qty.Sub(decimal.NewFromInt(1))
	}
	return Trade{}, false
}

func sortedBars(input BenchmarkInput) ([]Bar, error) {
	if len(input.Bars) == 0 {
		return nil, errors.New("基準回測需要歷史行情")
	}
	if input.InitialCash.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("期初資金必須大於 0")
	}
	bars := make([]Bar, len(input.Bars))
	for i, bar := range input.Bars {
		bars[i] = Bar{Date: normalizeDate(bar.Date), Close: bar.Close}
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date.Before(bars[j].Date) })
	return bars, nil
}

func finishBenchmark(result *Result, symbol string, cash, shares decimal.Decimal) {
	result.FinalCash = cash
	result.FinalPositions = map[string]decimal.Decimal{}
	if shares.GreaterThan(decimal.Zero) {
		result.FinalPositions[symbol] = shares
	}
	result.FinalEquity = result.EquityCurve[len(result.EquityCurve)-1].Equity
	fillMetrics(result)
}
