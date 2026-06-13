package ledger

import (
	"context"
	"database/sql"
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

func (s *Service) PortfolioHistory(ctx context.Context, userID, from, to string, limit int) ([]PortfolioHistoryPoint, error) {
	if limit <= 0 || limit > 500 {
		limit = 180
	}
	rows, err := s.db.Query(ctx, `
		WITH user_bounds AS (
			SELECT
				COALESCE(
					NULLIF($2, '')::date,
					(SELECT min(trade_date) FROM ledger_events WHERE user_id = $1 AND voided_at IS NULL),
					CURRENT_DATE - INTERVAL '90 days'
				)::date AS from_date,
				COALESCE(NULLIF($3, '')::date, CURRENT_DATE)::date AS to_date
		),
		days_desc AS (
			SELECT c.cal_date
			FROM trading_calendar c
			CROSS JOIN user_bounds b
			WHERE c.market = 'TWSE'
			  AND c.is_open
			  AND c.cal_date BETWEEN b.from_date AND b.to_date
			ORDER BY c.cal_date DESC
			LIMIT $4
		),
		days AS (
			SELECT cal_date FROM days_desc
		),
		cash_by_day AS (
			SELECT d.cal_date, COALESCE(sum(e.cash_delta), 0) AS cash
			FROM days d
			LEFT JOIN ledger_events e
			  ON e.user_id = $1
			 AND e.voided_at IS NULL
			 AND e.trade_date <= d.cal_date
			GROUP BY d.cal_date
		),
		positions AS (
			SELECT d.cal_date, e.asset_id, sum(COALESCE(e.quantity_shares, 0)) AS quantity
			FROM days d
			JOIN ledger_events e
			  ON e.user_id = $1
			 AND e.voided_at IS NULL
			 AND e.asset_id IS NOT NULL
			 AND e.trade_date <= d.cal_date
			GROUP BY d.cal_date, e.asset_id
			HAVING sum(COALESCE(e.quantity_shares, 0)) <> 0
		),
		priced_positions AS (
			SELECT p.cal_date, a.symbol, p.quantity, b.bar_date, b.close
			FROM positions p
			JOIN assets a ON a.id = p.asset_id
			LEFT JOIN LATERAL (
				SELECT mb.bar_date, mb.close
				FROM market_daily_bars mb
				WHERE mb.asset_id = p.asset_id
				  AND mb.is_latest
				  AND mb.bar_date <= p.cal_date
				ORDER BY mb.bar_date DESC, mb.revision DESC
				LIMIT 1
			) b ON true
		),
		aggregated AS (
			SELECT
				d.cal_date,
				c.cash,
				COALESCE(sum(CASE WHEN p.close IS NOT NULL THEN p.quantity * p.close ELSE 0 END), 0) AS market_value,
				min(p.bar_date) FILTER (WHERE p.bar_date IS NOT NULL) AS market_data_as_of,
				COALESCE(
					array_agg(p.symbol ORDER BY p.symbol) FILTER (WHERE p.close IS NULL),
					ARRAY[]::text[]
				) AS missing_price_symbols,
				COALESCE(
					array_agg(p.symbol ORDER BY p.symbol) FILTER (WHERE p.close IS NOT NULL AND p.bar_date < d.cal_date),
					ARRAY[]::text[]
				) AS stale_price_symbols
			FROM days d
			JOIN cash_by_day c ON c.cal_date = d.cal_date
			LEFT JOIN priced_positions p ON p.cal_date = d.cal_date
			GROUP BY d.cal_date, c.cash
		)
		SELECT
			cal_date::text,
			cash::text,
			market_value::text,
			(cash + market_value)::text,
			market_data_as_of::text,
			missing_price_symbols,
			stale_price_symbols,
			cardinality(missing_price_symbols) = 0
		FROM aggregated
		ORDER BY cal_date
	`, userID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []PortfolioHistoryPoint
	for rows.Next() {
		var point PortfolioHistoryPoint
		var marketDataAsOf sql.NullString
		if err := rows.Scan(
			&point.Date,
			&point.Cash,
			&point.MarketValue,
			&point.TotalValue,
			&marketDataAsOf,
			&point.MissingPriceSymbols,
			&point.StalePriceSymbols,
			&point.IsComplete,
		); err != nil {
			return nil, err
		}
		if marketDataAsOf.Valid {
			point.MarketDataAsOf = &marketDataAsOf.String
		}
		points = append(points, point)
	}
	return points, rows.Err()
}
