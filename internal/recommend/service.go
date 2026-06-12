package recommend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/ledger"
	"github.com/tingz/easy-invest/internal/marketdata"
	"github.com/tingz/easy-invest/internal/num"
	"github.com/tingz/easy-invest/internal/strategy"
	"github.com/tingz/easy-invest/internal/twmarket"
)

const Disclaimer = "本系統提供之內容為依使用者輸入資料與歷史市場資料計算之參考資訊，不構成投資建議或要約，不保證獲利。投資人應自行判斷並承擔投資風險。"

type Service struct {
	db     *pgxpool.Pool
	ledger *ledger.Service
	market *marketdata.Service
}

type Run struct {
	ID             string            `json:"id"`
	Strategy       map[string]string `json:"strategy"`
	MarketDataAsOf string            `json:"market_data_as_of"`
	PortfolioAsOf  string            `json:"portfolio_as_of"`
	Disclaimer     string            `json:"disclaimer"`
	Status         string            `json:"status"`
	CreatedAt      string            `json:"created_at"`
	Items          []Item            `json:"items,omitempty"`
}

type Item struct {
	ID             string  `json:"id"`
	RunID          string  `json:"run_id,omitempty"`
	AssetID        *string `json:"asset_id,omitempty"`
	Symbol         *string `json:"symbol,omitempty"`
	Action         string  `json:"action"`
	QuantityShares *string `json:"quantity_shares,omitempty"`
	EstPrice       *string `json:"est_price,omitempty"`
	EstAmount      *string `json:"est_amount,omitempty"`
	EstFeeTax      *string `json:"est_fee_tax,omitempty"`
	CurrentWeight  *string `json:"current_weight,omitempty"`
	TargetWeight   *string `json:"target_weight,omitempty"`
	Reason         string  `json:"reason"`
	Risks          string  `json:"risks"`
	Confidence     *string `json:"confidence,omitempty"`
	UserStatus     string  `json:"user_status"`
}

type Override struct {
	TargetWeights map[string]decimal.Decimal
}

func NewService(db *pgxpool.Pool, ledgerSvc *ledger.Service, marketSvc *marketdata.Service) *Service {
	return &Service{db: db, ledger: ledgerSvc, market: marketSvc}
}

func (s *Service) CreateRun(ctx context.Context, userID string, override Override) (Run, error) {
	settings, err := s.settings(ctx, userID)
	if err != nil {
		return Run{}, err
	}
	if len(override.TargetWeights) > 0 {
		settings.TargetWeights = override.TargetWeights
	}
	portfolio, err := s.ledger.Portfolio(ctx, userID)
	if err != nil {
		return Run{}, err
	}
	symbolSet := map[string]bool{}
	for symbol := range settings.TargetWeights {
		if symbol != "cash" {
			symbolSet[symbol] = true
		}
	}
	for _, p := range portfolio.Positions {
		symbolSet[p.Symbol] = true
	}
	var symbols []string
	for symbol := range symbolSet {
		symbols = append(symbols, symbol)
	}
	prices, err := s.market.LatestPrices(ctx, symbols)
	if err != nil {
		return Run{}, err
	}

	positionBySymbol := map[string]ledger.Position{}
	for _, p := range portfolio.Positions {
		positionBySymbol[p.Symbol] = p
	}
	var strategyPositions []strategy.Position
	var marketDataAsOf time.Time
	for _, symbol := range symbols {
		price, ok := prices[symbol]
		if !ok {
			if p, exists := positionBySymbol[symbol]; exists {
				strategyPositions = append(strategyPositions, strategy.Position{
					Symbol: symbol, AssetID: p.AssetID, AssetType: p.AssetType, LotSize: p.LotSize,
					QuantityShares: num.Parse(p.QuantityShares), Price: decimal.Zero,
				})
			}
			continue
		}
		qty := decimal.Zero
		if p, exists := positionBySymbol[symbol]; exists {
			qty = num.Parse(p.QuantityShares)
		}
		strategyPositions = append(strategyPositions, strategy.Position{
			Symbol: symbol, AssetID: price.AssetID, AssetType: price.AssetType, LotSize: price.LotSize,
			QuantityShares: qty, Price: price.Close,
		})
		if marketDataAsOf.IsZero() || price.Date.Before(marketDataAsOf) {
			marketDataAsOf = price.Date
		}
	}
	if marketDataAsOf.IsZero() {
		marketDataAsOf = time.Now()
	}

	intents := strategy.Rebalance(strategy.Input{
		Cash:      num.Parse(portfolio.Cash),
		Positions: strategyPositions,
		Settings: strategy.Settings{
			TargetWeights:  settings.TargetWeights,
			RebalanceBand:  settings.RebalanceBand,
			CashBuffer:     settings.CashBuffer,
			MinTradeAmount: settings.MinTradeAmount,
			PreferWholeLot: settings.PreferWholeLot,
		},
	})
	snapshotID, err := s.ledger.CreateSnapshot(ctx, userID, "recommendation_input")
	if err != nil {
		return Run{}, err
	}
	strategyID, strategyName, strategyVersion, err := s.strategyVersion(ctx)
	if err != nil {
		return Run{}, err
	}

	inputsJSON, _ := json.Marshal(map[string]any{
		"portfolio": portfolio,
		"settings":  settings,
		"override":  override,
	})
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)

	var run Run
	err = tx.QueryRow(ctx, `
		INSERT INTO recommendation_runs (user_id, strategy_version_id, portfolio_snapshot_id, market_data_as_of, inputs)
		VALUES ($1, $2, $3, $4::date, $5::jsonb)
		RETURNING id::text, status, created_at::text
	`, userID, strategyID, snapshotID, marketDataAsOf.Format("2006-01-02"), string(inputsJSON)).
		Scan(&run.ID, &run.Status, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	run.Strategy = map[string]string{"name": strategyName, "version": strategyVersion}
	run.MarketDataAsOf = marketDataAsOf.Format("2006-01-02")
	run.PortfolioAsOf = portfolio.AsOf
	run.Disclaimer = Disclaimer

	for _, intent := range intents {
		item, err := s.insertItem(ctx, tx, run.ID, intent, settings)
		if err != nil {
			return Run{}, err
		}
		run.Items = append(run.Items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Service) ListRuns(ctx context.Context, userID string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, sv.name, sv.version, r.market_data_as_of::text, ps.as_of::text, r.status, r.created_at::text
		FROM recommendation_runs r
		JOIN strategy_versions sv ON sv.id = r.strategy_version_id
		JOIN portfolio_snapshots ps ON ps.id = r.portfolio_snapshot_id
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var run Run
		var name, version string
		if err := rows.Scan(&run.ID, &name, &version, &run.MarketDataAsOf, &run.PortfolioAsOf, &run.Status, &run.CreatedAt); err != nil {
			return nil, err
		}
		run.Strategy = map[string]string{"name": name, "version": version}
		run.Disclaimer = Disclaimer
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Service) GetRun(ctx context.Context, userID, runID string) (Run, error) {
	var run Run
	var name, version string
	err := s.db.QueryRow(ctx, `
		SELECT r.id::text, sv.name, sv.version, r.market_data_as_of::text, ps.as_of::text, r.status, r.created_at::text
		FROM recommendation_runs r
		JOIN strategy_versions sv ON sv.id = r.strategy_version_id
		JOIN portfolio_snapshots ps ON ps.id = r.portfolio_snapshot_id
		WHERE r.user_id = $1 AND r.id = $2
	`, userID, runID).Scan(&run.ID, &name, &version, &run.MarketDataAsOf, &run.PortfolioAsOf, &run.Status, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	run.Strategy = map[string]string{"name": name, "version": version}
	run.Disclaimer = Disclaimer
	items, err := s.items(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	run.Items = items
	return run, nil
}

func (s *Service) UpdateItemStatus(ctx context.Context, userID, itemID, status string) (Item, error) {
	if status != "viewed" && status != "accepted" && status != "ignored" && status != "expired" {
		return Item{}, errors.New("invalid status")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE recommendation_items i
		SET user_status = $3, status_changed_at = now()
		FROM recommendation_runs r
		WHERE i.run_id = r.id AND r.user_id = $1 AND i.id = $2
	`, userID, itemID, status)
	if err != nil {
		return Item{}, err
	}
	if tag.RowsAffected() == 0 {
		return Item{}, pgx.ErrNoRows
	}
	return s.item(ctx, itemID)
}

type settings struct {
	FeeRate             decimal.Decimal
	FeeDiscount         decimal.Decimal
	FeeMinimum          decimal.Decimal
	DividendTransferFee decimal.Decimal
	CashBuffer          decimal.Decimal
	MinTradeAmount      decimal.Decimal
	PreferWholeLot      bool
	RiskProfile         string
	TargetWeights       map[string]decimal.Decimal
	RebalanceBand       decimal.Decimal
}

func (s *Service) settings(ctx context.Context, userID string) (settings, error) {
	var row settings
	var feeRate, feeDiscount, feeMinimum, dividendFee, cashBuffer, minTrade, rebalanceBand string
	var targetJSON []byte
	err := s.db.QueryRow(ctx, `
		SELECT fee_rate::text, fee_discount::text, fee_minimum::text, dividend_transfer_fee::text,
		       cash_buffer::text, min_trade_amount::text, prefer_whole_lot, risk_profile,
		       target_weights, rebalance_band::text
		FROM user_settings WHERE user_id = $1
	`, userID).Scan(&feeRate, &feeDiscount, &feeMinimum, &dividendFee, &cashBuffer, &minTrade, &row.PreferWholeLot, &row.RiskProfile, &targetJSON, &rebalanceBand)
	if err != nil {
		return settings{}, err
	}
	row.FeeRate = num.Parse(feeRate)
	row.FeeDiscount = num.Parse(feeDiscount)
	row.FeeMinimum = num.Parse(feeMinimum)
	row.DividendTransferFee = num.Parse(dividendFee)
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

func (s *Service) strategyVersion(ctx context.Context) (id, name, version string, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT id::text, name, version
		FROM strategy_versions
		WHERE name = 'target_weight_rebalance' AND version = '1.0.0'
	`).Scan(&id, &name, &version)
	return id, name, version, err
}

func (s *Service) insertItem(ctx context.Context, tx pgx.Tx, runID string, intent strategy.Intent, settings settings) (Item, error) {
	feeTax := decimal.Zero
	if intent.EstimatedAmount.GreaterThan(decimal.Zero) {
		feeSettings := twmarket.FeeSettings{FeeRate: settings.FeeRate, FeeDiscount: settings.FeeDiscount, FeeMinimum: settings.FeeMinimum, DividendTransferFee: settings.DividendTransferFee}
		feeTax = twmarket.EstimateFee(intent.EstimatedAmount, feeSettings)
		if intent.Action == "sell" {
			feeTax = feeTax.Add(twmarket.EstimateSecuritiesTax(intent.EstimatedAmount, twmarket.AssetTWStock))
		}
	}
	var assetID any
	if intent.AssetID != "" {
		assetID = intent.AssetID
	}
	var item Item
	err := tx.QueryRow(ctx, `
		INSERT INTO recommendation_items (
			run_id, asset_id, action, quantity_shares, est_price, est_amount, est_fee_tax,
			current_weight, target_weight, reason, risks, confidence
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4::numeric, $5::numeric, $6::numeric, $7::numeric,
		        $8::numeric, $9::numeric, $10, $11, $12::numeric)
		RETURNING id::text, user_status
	`, runID, stringOrEmpty(assetID), intent.Action,
		nullableDecimal(intent.QuantityShares), nullableDecimal(intent.EstimatedPrice), nullableDecimal(intent.EstimatedAmount), nullableDecimal(feeTax),
		nullableDecimal(intent.CurrentWeight), nullableDecimal(intent.TargetWeight), intent.Reason, intent.Risks, nullableDecimal(intent.Confidence)).
		Scan(&item.ID, &item.UserStatus)
	if err != nil {
		return Item{}, err
	}
	item.RunID = runID
	item.Action = intent.Action
	item.Reason = intent.Reason
	item.Risks = intent.Risks
	item.Symbol = stringPtr(intent.Symbol)
	item.AssetID = stringPtr(intent.AssetID)
	item.QuantityShares = decimalPtrString(intent.QuantityShares)
	item.EstPrice = decimalPtrString(intent.EstimatedPrice)
	item.EstAmount = decimalPtrString(intent.EstimatedAmount)
	item.EstFeeTax = decimalPtrString(feeTax)
	item.CurrentWeight = decimalPtrString(intent.CurrentWeight)
	item.TargetWeight = decimalPtrString(intent.TargetWeight)
	item.Confidence = decimalPtrString(intent.Confidence)
	return item, nil
}

func (s *Service) items(ctx context.Context, runID string) ([]Item, error) {
	rows, err := s.db.Query(ctx, `
		SELECT i.id::text, i.run_id::text, i.asset_id::text, a.symbol,
		       i.action, i.quantity_shares::text, i.est_price::text, i.est_amount::text, i.est_fee_tax::text,
		       i.current_weight::text, i.target_weight::text, i.reason, i.risks, i.confidence::text, i.user_status
		FROM recommendation_items i
		LEFT JOIN assets a ON a.id = i.asset_id
		WHERE i.run_id = $1
		ORDER BY i.created_at, i.id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) item(ctx context.Context, itemID string) (Item, error) {
	return scanItem(s.db.QueryRow(ctx, `
		SELECT i.id::text, i.run_id::text, i.asset_id::text, a.symbol,
		       i.action, i.quantity_shares::text, i.est_price::text, i.est_amount::text, i.est_fee_tax::text,
		       i.current_weight::text, i.target_weight::text, i.reason, i.risks, i.confidence::text, i.user_status
		FROM recommendation_items i
		LEFT JOIN assets a ON a.id = i.asset_id
		WHERE i.id = $1
	`, itemID))
}

type scanner interface{ Scan(...any) error }

func scanItem(row scanner) (Item, error) {
	var item Item
	var assetID, symbol, quantity, price, amount, feeTax, currentWeight, targetWeight, confidence sql.NullString
	if err := row.Scan(&item.ID, &item.RunID, &assetID, &symbol, &item.Action, &quantity, &price, &amount, &feeTax, &currentWeight, &targetWeight, &item.Reason, &item.Risks, &confidence, &item.UserStatus); err != nil {
		return Item{}, err
	}
	item.AssetID = nullStringPtr(assetID)
	item.Symbol = nullStringPtr(symbol)
	item.QuantityShares = nullStringPtr(quantity)
	item.EstPrice = nullStringPtr(price)
	item.EstAmount = nullStringPtr(amount)
	item.EstFeeTax = nullStringPtr(feeTax)
	item.CurrentWeight = nullStringPtr(currentWeight)
	item.TargetWeight = nullStringPtr(targetWeight)
	item.Confidence = nullStringPtr(confidence)
	return item, nil
}

func nullableDecimal(d decimal.Decimal) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

func decimalPtrString(d decimal.Decimal) *string {
	if d.IsZero() {
		return nil
	}
	v := d.String()
	return &v
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
