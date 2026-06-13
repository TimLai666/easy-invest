package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/num"
	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// DefaultBenchmarkSymbol 是預設基準標的（元大台灣50）。
const DefaultBenchmarkSymbol = "0050"

// Service 負責回測的資料載入（market_daily_bars）與結果保存（backtest_runs）；
// 計算一律交給本套件的純函式核心，Service 不重複任何策略邏輯。
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// RunParams 是建立回測的參數；金額單位一律為 TWD、數量單位為股。
type RunParams struct {
	Name            string                     `json:"name"`
	Symbols         []string                   `json:"symbols"`          // 參與回測的標的；空值時取目標權重的 key
	From            string                     `json:"from"`             // YYYY-MM-DD，空值代表不限
	To              string                     `json:"to"`               // YYYY-MM-DD，空值代表不限
	InitialCash     decimal.Decimal            `json:"initial_cash"`     // 期初資金（TWD）
	TargetWeights   map[string]decimal.Decimal `json:"target_weights"`   // 空值時讀取 user_settings
	BenchmarkSymbol string                     `json:"benchmark_symbol"` // 預設 0050
	MonthlyAmount   decimal.Decimal            `json:"monthly_amount"`   // DCA 基準每月金額；零值時以期初資金均分月數
}

// Run 是 backtest_runs 的一筆紀錄。
type Run struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	Params       json.RawMessage `json:"params"`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	CreatedAt    string          `json:"created_at"`
}

// CreateRun 載入歷史行情、同步執行回測與兩種基準，並把結果存進 backtest_runs。
// 計算失敗時仍會留下 status=failed 的紀錄（含 error_message），回傳該筆 Run 而非 error。
func (s *Service) CreateRun(ctx context.Context, userID string, params RunParams) (Run, error) {
	if params.InitialCash.LessThanOrEqual(decimal.Zero) {
		return Run{}, errors.New("initial_cash 必須大於 0")
	}
	settings, err := s.loadSettings(ctx, userID)
	if err != nil {
		return Run{}, err
	}
	weights := params.TargetWeights
	if len(weights) == 0 {
		weights = settings.TargetWeights
	}
	if len(weights) == 0 {
		return Run{}, errors.New("尚未設定目標權重，無法回測")
	}
	if params.BenchmarkSymbol == "" {
		params.BenchmarkSymbol = DefaultBenchmarkSymbol
	}
	params.TargetWeights = weights

	strategySymbols := params.Symbols
	if len(strategySymbols) == 0 {
		for symbol := range weights {
			if symbol != "cash" {
				strategySymbols = append(strategySymbols, symbol)
			}
		}
	}
	sort.Strings(strategySymbols)
	params.Symbols = strategySymbols

	allSymbols := append([]string{}, strategySymbols...)
	if !containsString(allSymbols, params.BenchmarkSymbol) {
		allSymbols = append(allSymbols, params.BenchmarkSymbol)
	}
	bars, assets, err := s.loadBars(ctx, allSymbols, params.From, params.To)
	if err != nil {
		return Run{}, err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return Run{}, err
	}

	resultJSON, runErr := s.compute(params, settings, weights, strategySymbols, bars, assets)

	status := "completed"
	errorMessage := ""
	if runErr != nil {
		status = "failed"
		errorMessage = runErr.Error()
		resultJSON = nil
	}
	var run Run
	err = s.db.QueryRow(ctx, `
		INSERT INTO backtest_runs (user_id, name, params, status, result, error_message)
		VALUES ($1, $2, $3::jsonb, $4, $5::jsonb, NULLIF($6, ''))
		RETURNING id::text, name, status, params, COALESCE(result, 'null'::jsonb), COALESCE(error_message, ''), created_at::text
	`, userID, params.Name, string(paramsJSON), status, nullableJSON(resultJSON), errorMessage).
		Scan(&run.ID, &run.Name, &run.Status, &run.Params, &run.Result, &run.ErrorMessage, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	return run, nil
}

// GetRun 取得單筆回測（限本人）。
func (s *Service) GetRun(ctx context.Context, userID, runID string) (Run, error) {
	var run Run
	err := s.db.QueryRow(ctx, `
		SELECT id::text, name, status, params, COALESCE(result, 'null'::jsonb), COALESCE(error_message, ''), created_at::text
		FROM backtest_runs
		WHERE user_id = $1 AND id = $2
	`, userID, runID).Scan(&run.ID, &run.Name, &run.Status, &run.Params, &run.Result, &run.ErrorMessage, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	return run, nil
}

// ListRuns 依建立時間新到舊列出回測（限本人）；列表不含完整 result 以免回應過大。
func (s *Service) ListRuns(ctx context.Context, userID string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, name, status, params, COALESCE(error_message, ''), created_at::text
		FROM backtest_runs
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.Name, &run.Status, &run.Params, &run.ErrorMessage, &run.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// compute 組裝核心輸入並執行策略回測與兩種基準；本身不碰資料庫。
func (s *Service) compute(params RunParams, settings userSettings, weights map[string]decimal.Decimal, strategySymbols []string, bars map[string][]Bar, assets map[string]AssetMeta) (json.RawMessage, error) {
	strategyBars := map[string][]Bar{}
	for _, symbol := range strategySymbols {
		if series, ok := bars[symbol]; ok {
			strategyBars[symbol] = series
		}
	}
	if len(strategyBars) == 0 {
		return nil, errors.New("指定區間內沒有任何標的的歷史行情")
	}

	strategyResult, err := Simulate(Input{
		Bars:        strategyBars,
		Assets:      assets,
		InitialCash: params.InitialCash,
		Settings: strategy.Settings{
			TargetWeights:  weights,
			RebalanceBand:  settings.RebalanceBand,
			CashBuffer:     settings.CashBuffer,
			MinTradeAmount: settings.MinTradeAmount,
			PreferWholeLot: settings.PreferWholeLot,
		},
		Fees: settings.Fees,
	})
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"strategy": toResultJSON(strategyResult),
		"assumptions": "以日終收盤價成交（保守假設）；賣出扣手續費與證交稅、買進加計手續費；" +
			"每月第一個交易日再平衡。結果為歷史模擬，不代表未來績效。",
	}

	// 基準：行情齊備才計算，缺資料時在結果中註記而不是整筆失敗。
	benchmarkBars := bars[params.BenchmarkSymbol]
	if len(benchmarkBars) == 0 {
		payload["benchmark_note"] = "缺少基準標的 " + params.BenchmarkSymbol + " 的歷史行情，未計算基準。"
	} else {
		monthly := params.MonthlyAmount
		if monthly.LessThanOrEqual(decimal.Zero) {
			months := countMonths(benchmarkBars)
			if months > 0 {
				// 預設把期初資金均分到每個月，與策略回測投入同樣的總資金。
				monthly = params.InitialCash.Div(decimal.NewFromInt(int64(months))).RoundDown(0)
			}
		}
		benchmarkInput := BenchmarkInput{
			Symbol:      params.BenchmarkSymbol,
			Asset:       assets[params.BenchmarkSymbol],
			Bars:        benchmarkBars,
			InitialCash: params.InitialCash,
			Fees:        settings.Fees,
		}
		if buyHold, err := BuyAndHold(benchmarkInput); err == nil {
			payload["benchmark_buy_hold"] = toResultJSON(buyHold)
		}
		benchmarkInput.MonthlyAmount = monthly
		if dca, err := DCA(benchmarkInput); err == nil {
			payload["benchmark_dca"] = toResultJSON(dca)
			payload["benchmark_dca_monthly_amount"] = monthly.String()
		}
	}
	return json.Marshal(payload)
}

// loadBars 從 market_daily_bars 撈取最新版（is_latest）日 K 與標的資料。
// 市場資料為全域共用，無 user_id 欄位；使用者隔離由 backtest_runs 負責。
func (s *Service) loadBars(ctx context.Context, symbols []string, from, to string) (map[string][]Bar, map[string]AssetMeta, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.symbol, a.id::text, a.asset_type, a.lot_size, b.bar_date, b.close::text
		FROM market_daily_bars b
		JOIN assets a ON a.id = b.asset_id
		WHERE b.is_latest = true
		  AND a.symbol = ANY($1)
		  AND ($2 = '' OR b.bar_date >= $2::date)
		  AND ($3 = '' OR b.bar_date <= $3::date)
		ORDER BY a.symbol, b.bar_date
	`, symbols, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	bars := map[string][]Bar{}
	assets := map[string]AssetMeta{}
	for rows.Next() {
		var symbol, assetID, assetType, closeText string
		var lotSize int
		var bar Bar
		if err := rows.Scan(&symbol, &assetID, &assetType, &lotSize, &bar.Date, &closeText); err != nil {
			return nil, nil, err
		}
		bar.Close = num.Parse(closeText)
		bars[symbol] = append(bars[symbol], bar)
		assets[symbol] = AssetMeta{AssetID: assetID, AssetType: assetType, LotSize: lotSize}
	}
	return bars, assets, rows.Err()
}

// userSettings 是回測會用到的使用者設定子集。
type userSettings struct {
	Fees           twmarket.FeeSettings
	CashBuffer     decimal.Decimal
	MinTradeAmount decimal.Decimal
	PreferWholeLot bool
	TargetWeights  map[string]decimal.Decimal
	RebalanceBand  decimal.Decimal
}

func (s *Service) loadSettings(ctx context.Context, userID string) (userSettings, error) {
	var row userSettings
	var feeRate, feeDiscount, feeMinimum, dividendFee, cashBuffer, minTrade, rebalanceBand string
	var targetJSON []byte
	err := s.db.QueryRow(ctx, `
		SELECT fee_rate::text, fee_discount::text, fee_minimum::text, dividend_transfer_fee::text,
		       cash_buffer::text, min_trade_amount::text, prefer_whole_lot,
		       target_weights, rebalance_band::text
		FROM user_settings
		WHERE user_id = $1
	`, userID).Scan(&feeRate, &feeDiscount, &feeMinimum, &dividendFee, &cashBuffer, &minTrade, &row.PreferWholeLot, &targetJSON, &rebalanceBand)
	if err != nil {
		return userSettings{}, err
	}
	row.Fees = twmarket.FeeSettings{
		FeeRate:             num.Parse(feeRate),
		FeeDiscount:         num.Parse(feeDiscount),
		FeeMinimum:          num.Parse(feeMinimum),
		DividendTransferFee: num.Parse(dividendFee),
	}
	row.CashBuffer = num.Parse(cashBuffer)
	row.MinTradeAmount = num.Parse(minTrade)
	row.RebalanceBand = num.Parse(rebalanceBand)
	row.TargetWeights = parseWeights(targetJSON)
	return row, nil
}

func parseWeights(raw []byte) map[string]decimal.Decimal {
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	result := make(map[string]decimal.Decimal, len(generic))
	for symbol, value := range generic {
		switch v := value.(type) {
		case string:
			result[symbol] = num.Parse(v)
		case float64:
			result[symbol] = decimal.NewFromFloat(v)
		}
	}
	return result
}

// --- 結果序列化（金額一律以字串保存，避免浮點誤差） ---

type equityPointJSON struct {
	Date   string `json:"date"`
	Equity string `json:"equity"`
}

type tradeJSON struct {
	Date           string `json:"date"`
	Symbol         string `json:"symbol"`
	Action         string `json:"action"`
	QuantityShares string `json:"quantity_shares"`
	Price          string `json:"price"`
	GrossAmount    string `json:"gross_amount"`
	Fee            string `json:"fee"`
	Tax            string `json:"tax"`
	CashDelta      string `json:"cash_delta"`
}

type resultJSON struct {
	InitialEquity    string            `json:"initial_equity"`
	FinalEquity      string            `json:"final_equity"`
	TotalReturn      string            `json:"total_return"`
	AnnualizedReturn string            `json:"annualized_return"`
	MaxDrawdown      string            `json:"max_drawdown"`
	FinalCash        string            `json:"final_cash"`
	FinalPositions   map[string]string `json:"final_positions"`
	EquityCurve      []equityPointJSON `json:"equity_curve"`
	Trades           []tradeJSON       `json:"trades"`
}

func toResultJSON(result Result) resultJSON {
	out := resultJSON{
		InitialEquity:    result.InitialEquity.String(),
		FinalEquity:      result.FinalEquity.String(),
		TotalReturn:      result.TotalReturn.String(),
		AnnualizedReturn: result.AnnualizedReturn.String(),
		MaxDrawdown:      result.MaxDrawdown.String(),
		FinalCash:        result.FinalCash.String(),
		FinalPositions:   map[string]string{},
	}
	for symbol, qty := range result.FinalPositions {
		out.FinalPositions[symbol] = qty.String()
	}
	for _, point := range result.EquityCurve {
		out.EquityCurve = append(out.EquityCurve, equityPointJSON{Date: dateKey(point.Date), Equity: point.Equity.String()})
	}
	for _, trade := range result.Trades {
		out.Trades = append(out.Trades, tradeJSON{
			Date:           dateKey(trade.Date),
			Symbol:         trade.Symbol,
			Action:         trade.Action,
			QuantityShares: trade.QuantityShares.String(),
			Price:          trade.Price.String(),
			GrossAmount:    trade.GrossAmount.String(),
			Fee:            trade.Fee.String(),
			Tax:            trade.Tax.String(),
			CashDelta:      trade.CashDelta.String(),
		})
	}
	return out
}

func countMonths(bars []Bar) int {
	months := map[string]bool{}
	for _, bar := range bars {
		months[bar.Date.Format("2006-01")] = true
	}
	return len(months)
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
