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
	quantityNegative := false
	if input.Quantity != "" {
		if asset == nil && input.Unit == twmarket.UnitLot {
			return preparedEvent{}, fmt.Errorf("%w: lot unit requires asset", ErrValidation)
		}
		raw := num.Parse(input.Quantity)
		if raw.IsNegative() {
			// 只有調整事件允許負股數（FIFO 沖銷）；其他事件型別的方向由 event_type 決定
			if !IsAdjustmentEvent(input.EventType) {
				return preparedEvent{}, fmt.Errorf("%w: 此事件型別的 quantity 必須為正數", ErrValidation)
			}
			quantityNegative = true
			raw = raw.Abs()
		}
		lotSize := 1
		if asset != nil {
			lotSize = asset.LotSize
		}
		q, err := twmarket.Shares(raw, input.Unit, lotSize)
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
	case EventSplit:
		// 股票分割／反分割：比率放 metadata.split_ratio（新股數/舊股數），
		// 股數變化由系統依未平倉持股計算，不收使用者輸入的 quantity。
		if asset == nil {
			return preparedEvent{}, fmt.Errorf("%w: split 需要 symbol", ErrValidation)
		}
		if input.Quantity != "" {
			return preparedEvent{}, fmt.Errorf("%w: split 的股數由系統依分割比例計算，請勿輸入 quantity", ErrValidation)
		}
		ratio, ok, err := metadataDecimal(input.Metadata, MetaSplitRatio)
		if err != nil {
			return preparedEvent{}, err
		}
		if !ok {
			return preparedEvent{}, fmt.Errorf("%w: split 需要 metadata.split_ratio（新股數/舊股數）", ErrValidation)
		}
		if ratio.LessThanOrEqual(decimal.Zero) || ratio.Equal(decimal.NewFromInt(1)) {
			return preparedEvent{}, fmt.Errorf("%w: split_ratio 必須為正數且不等於 1", ErrValidation)
		}
		total, err := s.totalRemainingShares(ctx, input.UserID, asset.ID)
		if err != nil {
			return preparedEvent{}, err
		}
		if total.LessThanOrEqual(decimal.Zero) {
			return preparedEvent{}, fmt.Errorf("%w: split 需要該資產尚有未平倉批次", ErrValidation)
		}
		// quantity_shares 記建立當下的淨增股數（反分割為負），僅供顯示；
		// rebuild 一律以 metadata.split_ratio 對當時批次重算，不依賴這個欄位。
		quantity = total.Mul(ratio).Sub(total)
		// 分割不動現金、不收費稅；反分割畸零股找零另記現金事件
		fee = decimal.Zero
		tax = decimal.Zero
		cashDelta = decimal.Zero
	case EventFeeAdjustment, EventTaxAdjustment, EventBrokerReconciliationAdjustment, EventManualCorrection:
		// 調整事件三種作用（語意詳見 types.go metadata 常數說明）：
		// 1. 純現金調整：metadata.cash_delta（有號）優先，未提供退回 gross − fee − tax。
		// 2. 股數調整：quantity 正=補股（metadata.unit_cost 每股成本，預設 0）、
		//    負=FIFO 沖銷且 realized_pnl 記 0。
		// 3. 成本調整：metadata.adjusted_cost_delta 只動批次 adjusted_cost。
		if quantityAbs.GreaterThan(decimal.Zero) && asset == nil {
			return preparedEvent{}, fmt.Errorf("%w: 帶股數的調整事件需要 symbol", ErrValidation)
		}
		if quantityNegative {
			quantity = quantityAbs.Neg()
		}
		// 提前驗證 metadata 數字格式，避免事件寫入後才在批次計算時失敗
		if _, _, err := metadataDecimal(input.Metadata, MetaUnitCost); err != nil {
			return preparedEvent{}, err
		}
		if _, hasCostDelta, err := metadataDecimal(input.Metadata, MetaAdjustedCostDelta); err != nil {
			return preparedEvent{}, err
		} else if hasCostDelta && asset == nil {
			return preparedEvent{}, fmt.Errorf("%w: 帶 adjusted_cost_delta 的調整事件需要 symbol", ErrValidation)
		}
		if cd, hasCashDelta, err := metadataDecimal(input.Metadata, MetaCashDelta); err != nil {
			return preparedEvent{}, err
		} else if hasCashDelta {
			cashDelta = cd
		} else {
			cashDelta = gross.Sub(fee).Sub(tax)
		}
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
		// stock_dividend 依 docs/tw-market-rules.md 建零成本批次：
		// original_cost 與 adjusted_cost 都是 0，調整後成本視角靠
		// 「股數變多、總成本不變」自然稀釋平均成本，不需額外動作。
		if event.QuantityAbs.LessThanOrEqual(decimal.Zero) {
			return nil
		}
		return s.insertLotTx(ctx, tx, event, event.OriginalCost)
	case EventSell:
		lots, err := s.openLotStatesTx(ctx, tx, event.UserID, event.Asset.ID)
		if err != nil {
			return err
		}
		netProceeds := event.Gross.Sub(event.Fee).Sub(event.Tax)
		consumptions, err := consumeLotsFIFO(lots, event.QuantityAbs, netProceeds)
		if err != nil {
			return err
		}
		if err := s.insertConsumptionsTx(ctx, tx, event.ID, consumptions); err != nil {
			return err
		}
		return s.saveLotStatesTx(ctx, tx, event.UserID, lots)
	case EventCapitalReduction:
		// 減資：股數縮減沿用 FIFO 沖銷（original_cost 視角，既有邏輯不動）；
		// 退還淨現金另外按比例扣存續批次的 adjusted_cost（券商 App「退錢降成本」視角）。
		// 注意：FIFO 沖銷已按 remaining/original 比例帶走部分調整後成本，
		// 與部分券商的減資成本算法可能有差異，差額用對帳調整事件吸收。
		lots, err := s.openLotStatesTx(ctx, tx, event.UserID, event.Asset.ID)
		if err != nil {
			return err
		}
		if event.QuantityAbs.GreaterThan(decimal.Zero) {
			netProceeds := event.Gross.Sub(event.Fee).Sub(event.Tax)
			consumptions, err := consumeLotsFIFO(lots, event.QuantityAbs, netProceeds)
			if err != nil {
				return err
			}
			if err := s.insertConsumptionsTx(ctx, tx, event.ID, consumptions); err != nil {
				return err
			}
		}
		if event.CashDelta.GreaterThan(decimal.Zero) {
			deductAdjustedCostProRata(lots, event.CashDelta)
		}
		return s.saveLotStatesTx(ctx, tx, event.UserID, lots)
	case EventCashDividend:
		// 除息：實收股利（gross − 匯費 − 健保補充保費）按未平倉股數比例
		// 從各批次 adjusted_cost 扣除（券商 App「領息扣成本」視角）；original_cost 不動。
		if event.CashDelta.LessThanOrEqual(decimal.Zero) {
			return nil
		}
		lots, err := s.openLotStatesTx(ctx, tx, event.UserID, event.Asset.ID)
		if err != nil {
			return err
		}
		deductAdjustedCostProRata(lots, event.CashDelta)
		return s.saveLotStatesTx(ctx, tx, event.UserID, lots)
	case EventSplit:
		ratio, ok, err := metadataDecimal(event.Metadata, MetaSplitRatio)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: split 需要 metadata.split_ratio（新股數/舊股數）", ErrValidation)
		}
		lots, err := s.openLotStatesTx(ctx, tx, event.UserID, event.Asset.ID)
		if err != nil {
			return err
		}
		// rebuild 時若分割前的買進已被 void、沒有未平倉批次，分割就是無作用事件
		if _, err := applySplitToLots(lots, ratio); err != nil {
			return err
		}
		return s.saveLotStatesTx(ctx, tx, event.UserID, lots)
	case EventFeeAdjustment, EventTaxAdjustment, EventBrokerReconciliationAdjustment, EventManualCorrection:
		return s.applyAdjustmentToLots(ctx, tx, event)
	default:
		return nil
	}
}

// applyAdjustmentToLots 處理調整事件對批次的影響。
// 三種作用的語意詳見 types.go 的 metadata 常數說明；純現金調整不會進到這裡
//（無 symbol 時 applyEventToLots 直接略過，現金影響已寫在事件 cash_delta）。
func (s *Service) applyAdjustmentToLots(ctx context.Context, tx pgx.Tx, event preparedEvent) error {
	if event.QuantityAbs.GreaterThan(decimal.Zero) {
		unitCost, hasUnitCost, err := metadataDecimal(event.Metadata, MetaUnitCost)
		if err != nil {
			return err
		}
		if event.Quantity.IsNegative() {
			// 股數調整（負）：按 FIFO 沖銷，realized_pnl 記 0（調整不認列損益）
			lots, err := s.openLotStatesTx(ctx, tx, event.UserID, event.Asset.ID)
			if err != nil {
				return err
			}
			var unitCostPtr *decimal.Decimal
			if hasUnitCost {
				unitCostPtr = &unitCost
			}
			consumptions, err := consumeLotsForAdjustment(lots, event.QuantityAbs, unitCostPtr)
			if err != nil {
				return err
			}
			if err := s.insertConsumptionsTx(ctx, tx, event.ID, consumptions); err != nil {
				return err
			}
			if err := s.saveLotStatesTx(ctx, tx, event.UserID, lots); err != nil {
				return err
			}
		} else {
			// 股數調整（正）：補股，建立新批次；unit_cost 未提供視為零成本補股
			cost := decimal.Zero
			if hasUnitCost {
				cost = unitCost.Mul(event.QuantityAbs)
			}
			if err := s.insertLotTx(ctx, tx, event, cost); err != nil {
				return err
			}
		}
	}
	delta, ok, err := metadataDecimal(event.Metadata, MetaAdjustedCostDelta)
	if err != nil {
		return err
	}
	if ok && !delta.IsZero() {
		// 成本調整：只動 adjusted_cost；delta 正=調整後成本增加。
		// 重新載入批次，讓同一筆事件剛補的股也吃得到成本調整。
		lots, err := s.openLotStatesTx(ctx, tx, event.UserID, event.Asset.ID)
		if err != nil {
			return err
		}
		deductAdjustedCostProRata(lots, delta.Neg())
		return s.saveLotStatesTx(ctx, tx, event.UserID, lots)
	}
	return nil
}

// insertLotTx 建立新批次；adjusted_cost 與 original_cost 同值起算，
// 之後只有 adjusted_cost 會被公司行動與調整事件改動。
func (s *Service) insertLotTx(ctx context.Context, tx pgx.Tx, event preparedEvent, cost decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO lots (user_id, asset_id, opened_by_event_id, open_date, original_quantity, remaining_quantity, original_cost, adjusted_cost)
		VALUES ($1, $2, $3, $4::date, $5::numeric, $5::numeric, $6::numeric, $6::numeric)
	`, event.UserID, event.Asset.ID, event.ID, event.TradeDate, event.QuantityAbs.String(), cost.String())
	return err
}

// openLotStatesTx 把某資產的未平倉批次依 FIFO 順序載入記憶體，並鎖列避免並發改動。
// 排序鍵用「開倉事件」的建立時間而不是 lots.created_at：
// rebuild 會在同一交易內重建所有批次，lots.created_at 全部相同，無法保證順序；
// 事件的 created_at 在 void/rebuild 後仍然不變，FIFO 順序因此是確定的。
func (s *Service) openLotStatesTx(ctx context.Context, tx pgx.Tx, userID, assetID string) ([]*lotState, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.id::text, l.opened_by_event_id::text, l.open_date::text,
		       l.original_quantity::text, l.remaining_quantity::text,
		       l.original_cost::text, l.adjusted_cost::text
		FROM lots l
		JOIN ledger_events e ON e.id = l.opened_by_event_id
		WHERE l.user_id = $1 AND l.asset_id = $2 AND l.remaining_quantity > 0
		ORDER BY l.open_date, e.created_at, e.id
		FOR UPDATE OF l
	`, userID, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lots []*lotState
	for rows.Next() {
		var lot lotState
		var originalQty, remainingQty, originalCost, adjustedCost string
		if err := rows.Scan(&lot.ID, &lot.OpenedByEventID, &lot.OpenDate, &originalQty, &remainingQty, &originalCost, &adjustedCost); err != nil {
			return nil, err
		}
		lot.OriginalQuantity = num.Parse(originalQty)
		lot.RemainingQuantity = num.Parse(remainingQty)
		lot.OriginalCost = num.Parse(originalCost)
		lot.AdjustedCost = num.Parse(adjustedCost)
		lots = append(lots, &lot)
	}
	return lots, rows.Err()
}

// saveLotStatesTx 把引擎改過（Dirty）的批次寫回；remaining 歸零時標記平倉。
func (s *Service) saveLotStatesTx(ctx context.Context, tx pgx.Tx, userID string, lots []*lotState) error {
	for _, lot := range lots {
		if !lot.Dirty {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE lots
			SET original_quantity = $3::numeric,
			    remaining_quantity = $4::numeric,
			    adjusted_cost = $5::numeric,
			    closed_at = CASE WHEN $4::numeric <= 0 THEN COALESCE(closed_at, now()) ELSE NULL END
			WHERE id = $1 AND user_id = $2
		`, lot.ID, userID, lot.OriginalQuantity.String(), lot.RemainingQuantity.String(), lot.AdjustedCost.String()); err != nil {
			return err
		}
	}
	return nil
}

// insertConsumptionsTx 寫入沖銷明細（lot_consumptions）。
func (s *Service) insertConsumptionsTx(ctx context.Context, tx pgx.Tx, eventID string, consumptions []lotConsumption) error {
	for _, c := range consumptions {
		if _, err := tx.Exec(ctx, `
			INSERT INTO lot_consumptions (lot_id, consumed_by_event_id, quantity, cost_consumed, realized_pnl)
			VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric)
		`, c.LotID, eventID, c.Quantity.String(), c.CostConsumed.String(), c.RealizedPnL.String()); err != nil {
			return err
		}
	}
	return nil
}

// totalRemainingShares 回傳使用者某資產目前的未平倉股數合計。
func (s *Service) totalRemainingShares(ctx context.Context, userID, assetID string) (decimal.Decimal, error) {
	var text string
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(sum(remaining_quantity), 0)::text
		FROM lots
		WHERE user_id = $1 AND asset_id = $2 AND remaining_quantity > 0
	`, userID, assetID).Scan(&text)
	if err != nil {
		return decimal.Zero, err
	}
	return num.Parse(text), nil
}

// rebuildLotsTx 把使用者所有批次與沖銷明細砍掉，依未作廢事件流全量重放。
// 重放順序 = trade_date、created_at、id，與事件當初套用的順序一致；
// split 以 metadata.split_ratio 重算、cash_dividend 與減資退現以事件 cash_delta 重扣
// adjusted_cost、調整事件依 metadata 重放，因此 adjusted_cost 永遠可以從事件流完整重建。
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
