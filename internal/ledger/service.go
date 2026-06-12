package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/num"
	"github.com/tingz/easy-invest/internal/twmarket"
)

var (
	ErrValidation         = errors.New("validation failed")
	ErrInsufficientShares = errors.New("insufficient shares")
)

type Service struct {
	db *pgxpool.Pool
}

type assetRow struct {
	ID        string
	Symbol    string
	Name      string
	AssetType string
	Market    string
	Currency  string
	LotSize   int
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) CreateEvent(ctx context.Context, input CreateEventInput) (Event, error) {
	event, err := s.prepareEvent(ctx, input)
	if err != nil {
		return Event{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(ctx)

	created, err := s.insertPreparedEvent(ctx, tx, event)
	if err != nil {
		return Event{}, err
	}
	if err := s.applyEventToLots(ctx, tx, created); err != nil {
		return Event{}, err
	}
	if _, err := s.createSnapshotTx(ctx, tx, input.UserID, "latest"); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Event{}, err
	}
	return created.Event, nil
}

func (s *Service) ListEvents(ctx context.Context, userID, symbol, eventType, from, to string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT e.id::text, e.user_id::text, e.asset_id::text, a.symbol,
		       e.event_type, e.trade_date::text, e.settlement_date::text,
		       e.quantity_shares::text, CASE WHEN a.lot_size IS NULL OR e.quantity_shares IS NULL THEN NULL ELSE (e.quantity_shares / a.lot_size)::text END,
		       e.price::text, e.gross_amount::text, e.fee::text, e.tax::text, e.cash_delta::text,
		       e.currency, e.fee_source, e.source, e.source_ref, e.notes, e.metadata,
		       e.created_at::text, e.voided_at::text, e.void_reason
		FROM ledger_events e
		LEFT JOIN assets a ON a.id = e.asset_id
		WHERE e.user_id = $1
		  AND ($2 = '' OR a.symbol = $2)
		  AND ($3 = '' OR e.event_type = $3)
		  AND ($4 = '' OR e.trade_date >= $4::date)
		  AND ($5 = '' OR e.trade_date <= $5::date)
		ORDER BY e.trade_date DESC, e.created_at DESC
		LIMIT $6
	`, userID, symbol, eventType, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Service) GetEvent(ctx context.Context, userID, id string) (Event, error) {
	row := s.db.QueryRow(ctx, `
		SELECT e.id::text, e.user_id::text, e.asset_id::text, a.symbol,
		       e.event_type, e.trade_date::text, e.settlement_date::text,
		       e.quantity_shares::text, CASE WHEN a.lot_size IS NULL OR e.quantity_shares IS NULL THEN NULL ELSE (e.quantity_shares / a.lot_size)::text END,
		       e.price::text, e.gross_amount::text, e.fee::text, e.tax::text, e.cash_delta::text,
		       e.currency, e.fee_source, e.source, e.source_ref, e.notes, e.metadata,
		       e.created_at::text, e.voided_at::text, e.void_reason
		FROM ledger_events e
		LEFT JOIN assets a ON a.id = e.asset_id
		WHERE e.user_id = $1 AND e.id = $2
	`, userID, id)
	return scanEvent(row)
}

func (s *Service) VoidEvent(ctx context.Context, userID, eventID, reason string) error {
	if reason == "" {
		return fmt.Errorf("%w: reason required", ErrValidation)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE ledger_events
		SET voided_at = now(), void_reason = $3
		WHERE id = $1 AND user_id = $2 AND voided_at IS NULL
	`, eventID, userID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err := s.rebuildLotsTx(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := s.createSnapshotTx(ctx, tx, userID, "latest"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Lots(ctx context.Context, userID, symbol string) ([]Lot, error) {
	rows, err := s.db.Query(ctx, `
		SELECT l.id::text, l.asset_id::text, a.symbol, l.opened_by_event_id::text,
		       l.open_date::text, l.original_quantity::text, l.remaining_quantity::text,
		       l.original_cost::text, l.adjusted_cost::text, l.closed_at::text
		FROM lots l
		JOIN assets a ON a.id = l.asset_id
		WHERE l.user_id = $1 AND ($2 = '' OR a.symbol = $2)
		ORDER BY a.symbol, l.open_date, l.created_at
	`, userID, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lots []Lot
	for rows.Next() {
		var lot Lot
		var closed sql.NullString
		if err := rows.Scan(&lot.ID, &lot.AssetID, &lot.Symbol, &lot.OpenedByEventID, &lot.OpenDate, &lot.OriginalQuantity, &lot.RemainingQuantity, &lot.OriginalCost, &lot.AdjustedCost, &closed); err != nil {
			return nil, err
		}
		if closed.Valid {
			lot.ClosedAt = &closed.String
		}
		lots = append(lots, lot)
	}
	return lots, rows.Err()
}

func (s *Service) Portfolio(ctx context.Context, userID string) (Portfolio, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Portfolio{}, err
	}
	defer tx.Rollback(ctx)
	return s.portfolioTx(ctx, tx, userID)
}

type preparedEvent struct {
	Event
	Asset        *assetRow
	Quantity     decimal.Decimal
	QuantityAbs  decimal.Decimal
	Price        decimal.Decimal
	Gross        decimal.Decimal
	Fee          decimal.Decimal
	Tax          decimal.Decimal
	CashDelta    decimal.Decimal
	OriginalCost decimal.Decimal
}

func (s *Service) prepareEvent(ctx context.Context, input CreateEventInput) (preparedEvent, error) {
	if input.EventType == "" || input.TradeDate == "" {
		return preparedEvent{}, fmt.Errorf("%w: event_type and trade_date required", ErrValidation)
	}
	source := input.Source
	if source == "" {
		source = "manual"
	}
	settings, err := s.feeSettings(ctx, input.UserID)
	if err != nil {
		return preparedEvent{}, err
	}

	var asset *assetRow
	if input.Symbol != "" {
		row, err := s.assetBySymbol(ctx, input.Symbol)
		if err != nil {
			return preparedEvent{}, err
		}
		asset = &row
	}

	price := num.Parse(input.Price)
	quantityAbs := decimal.Zero
	if input.Quantity != "" {
		if asset == nil && input.Unit == twmarket.UnitLot {
			return preparedEvent{}, fmt.Errorf("%w: lot unit requires asset", ErrValidation)
		}
		lotSize := 1
		if asset != nil {
			lotSize = asset.LotSize
		}
		q, err := twmarket.Shares(num.Parse(input.Quantity), input.Unit, lotSize)
		if err != nil {
			return preparedEvent{}, fmt.Errorf("%w: %v", ErrValidation, err)
		}
		quantityAbs = q
	}

	gross := decimal.Zero
	if input.GrossAmount != nil && *input.GrossAmount != "" {
		gross = num.Parse(*input.GrossAmount)
	} else if quantityAbs.GreaterThan(decimal.Zero) && price.GreaterThan(decimal.Zero) {
		gross = twmarket.GrossAmount(quantityAbs, price)
	}

	fee := decimal.Zero
	tax := decimal.Zero
	feeSource := "user"
	if input.Fee != nil && *input.Fee != "" {
		fee = num.Parse(*input.Fee)
	} else {
		feeSource = "estimated"
		switch input.EventType {
		case EventBuy, EventSell:
			fee = twmarket.EstimateFee(gross, settings)
		case EventCashDividend:
			fee = settings.DividendTransferFee
		}
	}
	if input.Tax != nil && *input.Tax != "" {
		tax = num.Parse(*input.Tax)
	} else {
		switch input.EventType {
		case EventSell:
			assetType := twmarket.AssetTWStock
			if asset != nil {
				assetType = asset.AssetType
			}
			tax = twmarket.EstimateSecuritiesTax(gross, assetType)
		case EventCashDividend:
			tax = twmarket.DividendHealthPremium(gross)
		}
	}

	quantity := quantityAbs
	cashDelta := decimal.Zero
	originalCost := decimal.Zero
	switch input.EventType {
	case EventBuy:
		if asset == nil || quantityAbs.IsZero() || price.IsZero() {
			return preparedEvent{}, fmt.Errorf("%w: buy requires symbol quantity unit price", ErrValidation)
		}
		cashDelta = gross.Add(fee).Neg()
		originalCost = gross.Add(fee)
	case EventSell:
		if asset == nil || quantityAbs.IsZero() || price.IsZero() {
			return preparedEvent{}, fmt.Errorf("%w: sell requires symbol quantity unit price", ErrValidation)
		}
		quantity = quantityAbs.Neg()
		cashDelta = gross.Sub(fee).Sub(tax)
	case EventCashDeposit:
		cashDelta = gross
	case EventCashWithdraw:
		cashDelta = gross.Neg()
	case EventCashDividend:
		if asset == nil {
			return preparedEvent{}, fmt.Errorf("%w: cash_dividend requires symbol", ErrValidation)
		}
		cashDelta = gross.Sub(fee).Sub(tax)
	case EventStockDividend:
		if asset == nil || quantityAbs.IsZero() {
			return preparedEvent{}, fmt.Errorf("%w: stock_dividend requires symbol quantity unit", ErrValidation)
		}
		originalCost = decimal.Zero
	case EventCapitalReduction:
		if asset == nil {
			return preparedEvent{}, fmt.Errorf("%w: capital_reduction requires symbol", ErrValidation)
		}
		quantity = quantityAbs.Neg()
		cashDelta = gross.Sub(fee).Sub(tax)
	default:
		cashDelta = gross.Sub(fee).Sub(tax)
	}

	var assetID *string
	var symbol *string
	if asset != nil {
		assetID = &asset.ID
		symbol = &asset.Symbol
	}
	quantityText := quantity.String()
	grossText := gross.String()
	priceText := price.String()
	event := Event{
		UserID:         input.UserID,
		AssetID:        assetID,
		Symbol:         symbol,
		EventType:      input.EventType,
		TradeDate:      input.TradeDate,
		SettlementDate: input.SettlementDate,
		QuantityShares: &quantityText,
		Price:          &priceText,
		GrossAmount:    &grossText,
		Fee:            fee.String(),
		Tax:            tax.String(),
		CashDelta:      cashDelta.String(),
		Currency:       "TWD",
		FeeSource:      feeSource,
		Source:         source,
		SourceRef:      input.SourceRef,
		Notes:          input.Notes,
		Metadata:       input.Metadata,
	}
	if quantity.IsZero() {
		event.QuantityShares = nil
	}
	if price.IsZero() {
		event.Price = nil
	}
	if gross.IsZero() && input.GrossAmount == nil {
		event.GrossAmount = nil
	}
	return preparedEvent{Event: event, Asset: asset, Quantity: quantity, QuantityAbs: quantityAbs, Price: price, Gross: gross, Fee: fee, Tax: tax, CashDelta: cashDelta, OriginalCost: originalCost}, nil
}

func (s *Service) insertPreparedEvent(ctx context.Context, tx pgx.Tx, event preparedEvent) (preparedEvent, error) {
	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, _ := json.Marshal(metadata)
	var quantity, price, gross any
	if event.QuantityShares != nil {
		quantity = *event.QuantityShares
	}
	if event.Event.Price != nil {
		price = *event.Event.Price
	}
	if event.Event.GrossAmount != nil {
		gross = *event.Event.GrossAmount
	}
	var assetID any
	if event.AssetID != nil {
		assetID = *event.AssetID
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO ledger_events (
			user_id, asset_id, event_type, trade_date, settlement_date,
			quantity_shares, price, gross_amount, fee, tax, cash_delta,
			currency, fee_source, source, source_ref, notes, metadata
		)
		VALUES (
			$1, NULLIF($2, '')::uuid, $3, $4::date, NULLIF($5, '')::date,
			$6::numeric, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11::numeric,
			$12, $13, $14, $15, $16, $17::jsonb
		)
		RETURNING id::text, created_at::text
	`, event.UserID, stringOrEmpty(assetID), event.EventType, event.TradeDate, stringPtrValue(event.SettlementDate),
		quantity, price, gross, event.Fee.String(), event.Tax.String(), event.CashDelta.String(),
		event.Currency, event.FeeSource, event.Source, event.SourceRef, event.Notes, string(metadataJSON)).
		Scan(&event.ID, &event.CreatedAt)
	if err != nil {
		return preparedEvent{}, err
	}
	return event, nil
}

func (s *Service) applyEventToLots(ctx context.Context, tx pgx.Tx, event preparedEvent) error {
	if event.Asset == nil {
		return nil
	}
	switch event.EventType {
	case EventBuy, EventStockDividend:
		if event.QuantityAbs.LessThanOrEqual(decimal.Zero) {
			return nil
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO lots (user_id, asset_id, opened_by_event_id, open_date, original_quantity, remaining_quantity, original_cost, adjusted_cost)
			VALUES ($1, $2, $3, $4::date, $5::numeric, $5::numeric, $6::numeric, $6::numeric)
		`, event.UserID, event.Asset.ID, event.ID, event.TradeDate, event.QuantityAbs.String(), event.OriginalCost.String())
		return err
	case EventSell, EventCapitalReduction:
		if event.QuantityAbs.LessThanOrEqual(decimal.Zero) {
			return nil
		}
		return s.consumeFIFO(ctx, tx, event)
	default:
		return nil
	}
}

func (s *Service) consumeFIFO(ctx context.Context, tx pgx.Tx, event preparedEvent) error {
	rows, err := tx.Query(ctx, `
		SELECT id::text, remaining_quantity::text, original_quantity::text, original_cost::text
		FROM lots
		WHERE user_id = $1 AND asset_id = $2 AND remaining_quantity > 0
		ORDER BY open_date, created_at
		FOR UPDATE
	`, event.UserID, event.Asset.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	remaining := event.QuantityAbs
	for rows.Next() {
		if remaining.LessThanOrEqual(decimal.Zero) {
			break
		}
		var lotID, remainingText, originalQtyText, originalCostText string
		if err := rows.Scan(&lotID, &remainingText, &originalQtyText, &originalCostText); err != nil {
			return err
		}
		lotRemaining := num.Parse(remainingText)
		originalQty := num.Parse(originalQtyText)
		originalCost := num.Parse(originalCostText)
		consume := decimal.Min(remaining, lotRemaining)
		costConsumed := originalCost.Mul(consume).Div(originalQty)
		netProceedsPart := event.Gross.Sub(event.Fee).Sub(event.Tax).Mul(consume).Div(event.QuantityAbs)
		realized := netProceedsPart.Sub(costConsumed)
		if _, err := tx.Exec(ctx, `
			INSERT INTO lot_consumptions (lot_id, consumed_by_event_id, quantity, cost_consumed, realized_pnl)
			VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric)
		`, lotID, event.ID, consume.String(), costConsumed.String(), realized.String()); err != nil {
			return err
		}
		newRemaining := lotRemaining.Sub(consume)
		closedExpr := "NULL"
		if newRemaining.LessThanOrEqual(decimal.Zero) {
			closedExpr = "now()"
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE lots SET remaining_quantity = $2::numeric, closed_at = %s WHERE id = $1
		`, closedExpr), lotID, newRemaining.String()); err != nil {
			return err
		}
		remaining = remaining.Sub(consume)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if remaining.GreaterThan(decimal.Zero) {
		return ErrInsufficientShares
	}
	return nil
}

func (s *Service) rebuildLotsTx(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM lot_consumptions
		WHERE lot_id IN (SELECT id FROM lots WHERE user_id = $1)
	`, userID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM lots WHERE user_id = $1`, userID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT e.id::text, e.user_id::text, e.asset_id::text, a.symbol, a.name, a.asset_type, a.market, a.currency, a.lot_size,
		       e.event_type, e.trade_date::text, e.quantity_shares::text, e.price::text, e.gross_amount::text,
		       e.fee::text, e.tax::text, e.cash_delta::text, e.currency, e.fee_source, e.source, e.notes, e.metadata, e.created_at::text
		FROM ledger_events e
		LEFT JOIN assets a ON a.id = e.asset_id
		WHERE e.user_id = $1 AND e.voided_at IS NULL
		ORDER BY e.trade_date, e.created_at, e.id
	`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var prepared preparedEvent
		var assetID, symbol, name, assetType, market, currency sql.NullString
		var lotSize sql.NullInt64
		var q, price, gross, fee, tax, cashDelta string
		if err := rows.Scan(&prepared.ID, &prepared.UserID, &assetID, &symbol, &name, &assetType, &market, &currency, &lotSize,
			&prepared.EventType, &prepared.TradeDate, &q, &price, &gross, &fee, &tax, &cashDelta,
			&prepared.Currency, &prepared.FeeSource, &prepared.Source, &prepared.Notes, &prepared.Metadata, &prepared.CreatedAt); err != nil {
			return err
		}
		if assetID.Valid {
			prepared.AssetID = &assetID.String
			prepared.Symbol = &symbol.String
			prepared.Asset = &assetRow{ID: assetID.String, Symbol: symbol.String, Name: name.String, AssetType: assetType.String, Market: market.String, Currency: currency.String, LotSize: int(lotSize.Int64)}
		}
		prepared.Quantity = num.Parse(q)
		prepared.QuantityAbs = prepared.Quantity.Abs()
		prepared.Price = num.Parse(price)
		prepared.Gross = num.Parse(gross)
		prepared.Fee = num.Parse(fee)
		prepared.Tax = num.Parse(tax)
		prepared.CashDelta = num.Parse(cashDelta)
		if prepared.EventType == EventBuy {
			prepared.OriginalCost = prepared.Gross.Add(prepared.Fee)
		}
		if err := s.applyEventToLots(ctx, tx, prepared); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) feeSettings(ctx context.Context, userID string) (twmarket.FeeSettings, error) {
	var feeRate, feeDiscount, feeMinimum, dividendFee string
	err := s.db.QueryRow(ctx, `
		SELECT fee_rate::text, fee_discount::text, fee_minimum::text, dividend_transfer_fee::text
		FROM user_settings WHERE user_id = $1
	`, userID).Scan(&feeRate, &feeDiscount, &feeMinimum, &dividendFee)
	if err != nil {
		return twmarket.DefaultFeeSettings(), err
	}
	return twmarket.FeeSettings{
		FeeRate:             num.Parse(feeRate),
		FeeDiscount:         num.Parse(feeDiscount),
		FeeMinimum:          num.Parse(feeMinimum),
		DividendTransferFee: num.Parse(dividendFee),
	}, nil
}

func (s *Service) assetBySymbol(ctx context.Context, symbol string) (assetRow, error) {
	var asset assetRow
	err := s.db.QueryRow(ctx, `
		SELECT id::text, symbol, name, asset_type, market, currency, lot_size
		FROM assets
		WHERE symbol = $1 AND is_active = true
		ORDER BY market
		LIMIT 1
	`, symbol).Scan(&asset.ID, &asset.Symbol, &asset.Name, &asset.AssetType, &asset.Market, &asset.Currency, &asset.LotSize)
	return asset, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var assetID, symbol, settlement, quantity, lots, price, gross, sourceRef, voidedAt, voidReason sql.NullString
	if err := row.Scan(&event.ID, &event.UserID, &assetID, &symbol,
		&event.EventType, &event.TradeDate, &settlement, &quantity, &lots,
		&price, &gross, &event.Fee, &event.Tax, &event.CashDelta,
		&event.Currency, &event.FeeSource, &event.Source, &sourceRef, &event.Notes, &event.Metadata,
		&event.CreatedAt, &voidedAt, &voidReason); err != nil {
		return Event{}, err
	}
	if assetID.Valid {
		event.AssetID = &assetID.String
	}
	if symbol.Valid {
		event.Symbol = &symbol.String
	}
	if settlement.Valid {
		event.SettlementDate = &settlement.String
	}
	if quantity.Valid {
		event.QuantityShares = &quantity.String
	}
	if lots.Valid {
		event.QuantityLots = &lots.String
	}
	if price.Valid {
		event.Price = &price.String
	}
	if gross.Valid {
		event.GrossAmount = &gross.String
	}
	if sourceRef.Valid {
		event.SourceRef = &sourceRef.String
	}
	if voidedAt.Valid {
		event.VoidedAt = &voidedAt.String
	}
	if voidReason.Valid {
		event.VoidReason = &voidReason.String
	}
	return event, nil
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func stringOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339)
}
