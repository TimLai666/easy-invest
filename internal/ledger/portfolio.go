package ledger

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/num"
	"github.com/tingz/easy-invest/internal/twmarket"
)

func (s *Service) portfolioTx(ctx context.Context, tx pgx.Tx, userID string) (Portfolio, error) {
	var cashText string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(cash_delta), 0)::text
		FROM ledger_events
		WHERE user_id = $1 AND voided_at IS NULL
	`, userID).Scan(&cashText); err != nil {
		return Portfolio{}, err
	}
	rows, err := tx.Query(ctx, `
		WITH latest_price AS (
			SELECT DISTINCT ON (asset_id) asset_id, bar_date::text AS bar_date, close::text AS close
			FROM market_daily_bars
			WHERE is_latest = true
			ORDER BY asset_id, bar_date DESC, revision DESC
		)
		SELECT a.id::text, a.symbol, a.name, a.asset_type, a.market, a.currency, a.lot_size,
		       sum(l.remaining_quantity)::text,
		       sum(l.original_cost * l.remaining_quantity / NULLIF(l.original_quantity, 0))::text,
		       sum(l.adjusted_cost * l.remaining_quantity / NULLIF(l.original_quantity, 0))::text,
		       latest_price.close, latest_price.bar_date
		FROM lots l
		JOIN assets a ON a.id = l.asset_id
		LEFT JOIN latest_price ON latest_price.asset_id = a.id
		WHERE l.user_id = $1 AND l.remaining_quantity > 0
		GROUP BY a.id, a.symbol, a.name, a.asset_type, a.market, a.currency, a.lot_size, latest_price.close, latest_price.bar_date
		ORDER BY a.symbol
	`, userID)
	if err != nil {
		return Portfolio{}, err
	}
	defer rows.Close()
	var positions []Position
	var marketDates []string
	for rows.Next() {
		var p Position
		var qtyText, originalCostText, adjustedCostText string
		var marketPrice, marketDate *string
		if err := rows.Scan(&p.AssetID, &p.Symbol, &p.Name, &p.AssetType, &p.Market, &p.Currency, &p.LotSize,
			&qtyText, &originalCostText, &adjustedCostText, &marketPrice, &marketDate); err != nil {
			return Portfolio{}, err
		}
		qty := num.Parse(qtyText)
		originalCost := num.Parse(originalCostText)
		adjustedCost := num.Parse(adjustedCostText)
		p.QuantityShares = qty.String()
		p.QuantityLots = twmarket.Lots(qty, p.LotSize).String()
		p.OriginalCost = originalCost.String()
		p.AdjustedCost = adjustedCost.String()
		if qty.GreaterThan(decimal.Zero) {
			p.AverageCost = originalCost.Div(qty).String()
			p.AdjustedAverageCost = adjustedCost.Div(qty).String()
		}
		if marketPrice != nil {
			price := num.Parse(*marketPrice)
			value := qty.Mul(price)
			p.MarketPrice = marketPrice
			marketValue := value.String()
			p.MarketValue = &marketValue
			unrealized := value.Sub(originalCost).String()
			p.UnrealizedPnL = &unrealized
		}
		if marketDate != nil {
			p.MarketDataAsOf = marketDate
			marketDates = append(marketDates, *marketDate)
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return Portfolio{}, err
	}
	sort.Strings(marketDates)
	var marketDataAsOf *string
	if len(marketDates) > 0 {
		marketDataAsOf = &marketDates[0]
	}
	return Portfolio{
		UserID:         userID,
		AsOf:           nowText(),
		Cash:           cashText,
		Positions:      positions,
		MarketDataAsOf: marketDataAsOf,
	}, nil
}

func (s *Service) createSnapshotTx(ctx context.Context, tx pgx.Tx, userID, kind string) (string, error) {
	portfolio, err := s.portfolioTx(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	positionsJSON, _ := json.Marshal(portfolio.Positions)
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO portfolio_snapshots (user_id, as_of, kind, positions, cash)
		VALUES ($1, now(), $2, $3::jsonb, $4::numeric)
		RETURNING id::text
	`, userID, kind, string(positionsJSON), portfolio.Cash).Scan(&id)
	return id, err
}

func (s *Service) CreateSnapshot(ctx context.Context, userID, kind string) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	id, err := s.createSnapshotTx(ctx, tx, userID, kind)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}
