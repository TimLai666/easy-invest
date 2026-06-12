package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type TWSEImportResult struct {
	IngestionRunID string `json:"ingestion_run_id"`
	Status         string `json:"status"`
	Rows           int    `json:"rows"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

type twseDailyAllRow struct {
	Code         string `json:"Code"`
	Name         string `json:"Name"`
	TradeVolume  string `json:"TradeVolume"`
	TradeValue   string `json:"TradeValue"`
	OpeningPrice string `json:"OpeningPrice"`
	HighestPrice string `json:"HighestPrice"`
	LowestPrice  string `json:"LowestPrice"`
	ClosingPrice string `json:"ClosingPrice"`
}

func (s *Service) ImportTWSEDailyAll(ctx context.Context, sourceURL string) (TWSEImportResult, error) {
	started := time.Now()
	runID, err := s.createIngestionRun(ctx, sourceURL, "running", started, 0, "")
	if err != nil {
		return TWSEImportResult{}, err
	}
	result := TWSEImportResult{IngestionRunID: runID, Status: "running"}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		_ = s.finishIngestionRun(ctx, runID, "failed", 0, err.Error())
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		return result, err
	}
	req.Header.Set("User-Agent", "easy-invest/1.0")
	resp, err := client.Do(req)
	if err != nil {
		_ = s.finishIngestionRun(ctx, runID, "failed", 0, err.Error())
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("twse returned status %d", resp.StatusCode)
		_ = s.finishIngestionRun(ctx, runID, "failed", 0, msg)
		result.Status = "failed"
		result.ErrorMessage = msg
		return result, fmt.Errorf("%s", msg)
	}

	var rows []twseDailyAllRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		_ = s.finishIngestionRun(ctx, runID, "failed", 0, err.Error())
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		return result, err
	}
	imported, err := s.saveTWSEDailyAllRows(ctx, runID, rows)
	if err != nil {
		_ = s.finishIngestionRun(ctx, runID, "failed", imported, err.Error())
		result.Status = "failed"
		result.Rows = imported
		result.ErrorMessage = err.Error()
		return result, err
	}
	if err := s.finishIngestionRun(ctx, runID, "succeeded", imported, ""); err != nil {
		return result, err
	}
	result.Status = "succeeded"
	result.Rows = imported
	return result, nil
}

func (s *Service) createIngestionRun(ctx context.Context, sourceURL, status string, fetchedAt time.Time, rowCount int, errorMessage string) (string, error) {
	var runID string
	err := s.db.QueryRow(ctx, `
		INSERT INTO ingestion_runs (source_name, source_url, source_license, dataset, fetched_at, data_time, status, row_count, error_message)
		VALUES ('twse_openapi', $1, 'TWSE OpenAPI', 'daily_bars', $2, $2, $3, $4, NULLIF($5, ''))
		RETURNING id::text
	`, sourceURL, fetchedAt, status, rowCount, errorMessage).Scan(&runID)
	return runID, err
}

func (s *Service) finishIngestionRun(ctx context.Context, runID, status string, rowCount int, errorMessage string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE ingestion_runs
		SET status = $2, row_count = $3, error_message = NULLIF($4, '')
		WHERE id = $1
	`, runID, status, rowCount, errorMessage)
	return err
}

func (s *Service) saveTWSEDailyAllRows(ctx context.Context, runID string, rows []twseDailyAllRow) (int, error) {
	location, _ := time.LoadLocation("Asia/Taipei")
	barDate := time.Now().In(location).Format("2006-01-02")

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	imported := 0
	for _, row := range rows {
		symbol := strings.TrimSpace(row.Code)
		closePrice, ok := parseTWNumber(row.ClosingPrice)
		if symbol == "" || !ok || closePrice.LessThanOrEqual(decimal.Zero) {
			continue
		}
		assetID, err := upsertTWSEAsset(ctx, tx, symbol, strings.TrimSpace(row.Name))
		if err != nil {
			return imported, err
		}
		open, _ := parseTWNumber(row.OpeningPrice)
		high, _ := parseTWNumber(row.HighestPrice)
		low, _ := parseTWNumber(row.LowestPrice)
		volume, _ := parseTWNumber(row.TradeVolume)
		turnover, _ := parseTWNumber(row.TradeValue)
		var revision int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(max(revision), -1) + 1
			FROM market_daily_bars
			WHERE asset_id = $1 AND bar_date = $2::date
		`, assetID, barDate).Scan(&revision); err != nil {
			return imported, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE market_daily_bars SET is_latest = false
			WHERE asset_id = $1 AND bar_date = $2::date
		`, assetID, barDate); err != nil {
			return imported, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO market_daily_bars (
				asset_id, bar_date, open, high, low, close, volume_shares, turnover, ingestion_run_id, revision, is_latest
			)
			VALUES ($1, $2::date, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric, $9, $10, true)
		`, assetID, barDate, open.String(), high.String(), low.String(), closePrice.String(), volume.String(), turnover.String(), runID, revision); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, tx.Commit(ctx)
}

func upsertTWSEAsset(ctx context.Context, tx pgx.Tx, symbol, name string) (string, error) {
	assetType := "tw_stock"
	if strings.HasPrefix(symbol, "00") {
		assetType = "tw_etf"
	}
	if strings.HasSuffix(symbol, "B") || strings.Contains(name, "債") {
		assetType = "tw_bond_etf"
	}
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO assets (asset_type, symbol, name, market, currency, lot_size)
		VALUES ($1, $2, $3, 'TWSE', 'TWD', 1000)
		ON CONFLICT (market, symbol)
		DO UPDATE SET name = EXCLUDED.name, asset_type = EXCLUDED.asset_type, is_active = true
		RETURNING id::text
	`, assetType, symbol, name).Scan(&id)
	return id, err
}

func parseTWNumber(value string) (decimal.Decimal, bool) {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, "--", "")
	cleaned = strings.ReplaceAll(cleaned, "X", "")
	if cleaned == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(cleaned)
	return d, err == nil
}
