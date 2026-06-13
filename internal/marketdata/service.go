package marketdata

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/num"
)

type Service struct {
	db *pgxpool.Pool
}

type Asset struct {
	ID        string         `json:"id"`
	AssetType string         `json:"asset_type"`
	Symbol    string         `json:"symbol"`
	Name      string         `json:"name"`
	Market    string         `json:"market"`
	Currency  string         `json:"currency"`
	LotSize   int            `json:"lot_size"`
	IsActive  bool           `json:"is_active"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type DailyBar struct {
	ID           string `json:"id"`
	AssetID      string `json:"asset_id"`
	Symbol       string `json:"symbol"`
	BarDate      string `json:"bar_date"`
	Open         string `json:"open,omitempty"`
	High         string `json:"high,omitempty"`
	Low          string `json:"low,omitempty"`
	Close        string `json:"close"`
	VolumeShares string `json:"volume_shares,omitempty"`
	Turnover     string `json:"turnover,omitempty"`
	Revision     int    `json:"revision"`
}

type LatestPrice struct {
	AssetID   string
	Symbol    string
	AssetType string
	LotSize   int
	Date      time.Time
	Close     decimal.Decimal
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) FindAssetBySymbol(ctx context.Context, symbol string) (Asset, error) {
	var asset Asset
	err := s.db.QueryRow(ctx, `
		SELECT id::text, asset_type, symbol, name, market, currency, lot_size, is_active, metadata
		FROM assets
		WHERE symbol = $1 AND is_active = true
		ORDER BY market
		LIMIT 1
	`, symbol).Scan(&asset.ID, &asset.AssetType, &asset.Symbol, &asset.Name, &asset.Market, &asset.Currency, &asset.LotSize, &asset.IsActive, &asset.Metadata)
	return asset, err
}

func (s *Service) GetAsset(ctx context.Context, id string) (Asset, error) {
	var asset Asset
	err := s.db.QueryRow(ctx, `
		SELECT id::text, asset_type, symbol, name, market, currency, lot_size, is_active, metadata
		FROM assets
		WHERE id = $1
	`, id).Scan(&asset.ID, &asset.AssetType, &asset.Symbol, &asset.Name, &asset.Market, &asset.Currency, &asset.LotSize, &asset.IsActive, &asset.Metadata)
	return asset, err
}

func (s *Service) ListAssets(ctx context.Context, query, assetType string, limit int) ([]Asset, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, asset_type, symbol, name, market, currency, lot_size, is_active, metadata
		FROM assets
		WHERE is_active = true
		  AND ($1 = '' OR symbol ILIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR asset_type = $2)
		ORDER BY symbol
		LIMIT $3
	`, query, assetType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Asset
	for rows.Next() {
		var asset Asset
		if err := rows.Scan(&asset.ID, &asset.AssetType, &asset.Symbol, &asset.Name, &asset.Market, &asset.Currency, &asset.LotSize, &asset.IsActive, &asset.Metadata); err != nil {
			return nil, err
		}
		items = append(items, asset)
	}
	return items, rows.Err()
}

func (s *Service) ListBars(ctx context.Context, symbol, from, to string, limit int) ([]DailyBar, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.Query(ctx, `
		SELECT b.id::text, b.asset_id::text, a.symbol, b.bar_date::text,
		       b.open::text, b.high::text, b.low::text, b.close::text,
		       b.volume_shares::text, b.turnover::text, b.revision
		FROM market_daily_bars b
		JOIN assets a ON a.id = b.asset_id
		WHERE b.is_latest = true
		  AND ($1 = '' OR a.symbol = $1)
		  AND ($2 = '' OR b.bar_date >= $2::date)
		  AND ($3 = '' OR b.bar_date <= $3::date)
		ORDER BY b.bar_date DESC, a.symbol
		LIMIT $4
	`, symbol, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bars []DailyBar
	for rows.Next() {
		var bar DailyBar
		var open, high, low, volume, turnover sql.NullString
		if err := rows.Scan(&bar.ID, &bar.AssetID, &bar.Symbol, &bar.BarDate, &open, &high, &low, &bar.Close, &volume, &turnover, &bar.Revision); err != nil {
			return nil, err
		}
		bar.Open = open.String
		bar.High = high.String
		bar.Low = low.String
		bar.VolumeShares = volume.String
		bar.Turnover = turnover.String
		bars = append(bars, bar)
	}
	return bars, rows.Err()
}

func (s *Service) LatestPrices(ctx context.Context, symbols []string) (map[string]LatestPrice, error) {
	if len(symbols) == 0 {
		return map[string]LatestPrice{}, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (a.symbol)
		       a.id::text, a.symbol, a.asset_type, a.lot_size, b.bar_date, b.close::text
		FROM assets a
		JOIN market_daily_bars b ON b.asset_id = a.id AND b.is_latest = true
		WHERE a.symbol = ANY($1)
		ORDER BY a.symbol, b.bar_date DESC, b.revision DESC
	`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]LatestPrice, len(symbols))
	for rows.Next() {
		var price LatestPrice
		var closeText string
		if err := rows.Scan(&price.AssetID, &price.Symbol, &price.AssetType, &price.LotSize, &price.Date, &closeText); err != nil {
			return nil, err
		}
		price.Close = num.Parse(closeText)
		result[price.Symbol] = price
	}
	return result, rows.Err()
}

func (s *Service) LatestPriceForAsset(ctx context.Context, assetID string) (LatestPrice, error) {
	var price LatestPrice
	var closeText string
	err := s.db.QueryRow(ctx, `
		SELECT a.id::text, a.symbol, a.asset_type, a.lot_size, b.bar_date, b.close::text
		FROM assets a
		JOIN market_daily_bars b ON b.asset_id = a.id AND b.is_latest = true
		WHERE a.id = $1
		ORDER BY b.bar_date DESC, b.revision DESC
		LIMIT 1
	`, assetID).Scan(&price.AssetID, &price.Symbol, &price.AssetType, &price.LotSize, &price.Date, &closeText)
	if errors.Is(err, pgx.ErrNoRows) {
		return LatestPrice{}, err
	}
	if err != nil {
		return LatestPrice{}, err
	}
	price.Close = num.Parse(closeText)
	return price, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
