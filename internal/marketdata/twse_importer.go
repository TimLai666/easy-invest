package marketdata

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ImportResult 是單次匯入的結果摘要。
type ImportResult struct {
	IngestionRunID string `json:"ingestion_run_id"`
	Status         string `json:"status"`
	Rows           int    `json:"rows"`
	Skipped        int    `json:"skipped"` // 與既有最新版完全相同而未重複入庫的列數
	ErrorMessage   string `json:"error_message,omitempty"`
}

// twseDailyAllRow 是 TWSE OpenAPI STOCK_DAY_ALL 的單列格式。
// 實測（2026-06-12）：每列都有 Date 欄，民國年緊湊格式（例如 "1150611"）。
type twseDailyAllRow struct {
	Date         string `json:"Date"`
	Code         string `json:"Code"`
	Name         string `json:"Name"`
	TradeVolume  string `json:"TradeVolume"`
	TradeValue   string `json:"TradeValue"`
	OpeningPrice string `json:"OpeningPrice"`
	HighestPrice string `json:"HighestPrice"`
	LowestPrice  string `json:"LowestPrice"`
	ClosingPrice string `json:"ClosingPrice"`
}

// tpexDailyRow 是 TPEx OpenAPI 上櫃日收盤行情的單列格式。
// 實測（2026-06-12）：Date 同為民國年緊湊格式，當日約 14:00 後可取得。
type tpexDailyRow struct {
	Date              string `json:"Date"`
	Code              string `json:"SecuritiesCompanyCode"`
	Name              string `json:"CompanyName"`
	Close             string `json:"Close"`
	Open              string `json:"Open"`
	High              string `json:"High"`
	Low               string `json:"Low"`
	TradingShares     string `json:"TradingShares"`
	TransactionAmount string `json:"TransactionAmount"`
	TransactionNumber string `json:"TransactionNumber"`
}

// dailyBarRow 是各來源解析後的標準化日 K 列。
type dailyBarRow struct {
	Symbol   string
	Name     string
	Market   string
	Date     time.Time
	Open     decimal.Decimal
	High     decimal.Decimal
	Low      decimal.Decimal
	Close    decimal.Decimal
	Volume   decimal.Decimal
	Turnover decimal.Decimal
}

// ImportTWSEDailyAll 匯入上市當日全市場日成交（快照端點，僅當日，不可回補）。
// bar_date 一律使用資料列自帶的 Date 欄；缺 Date 的列改用最近一個已開盤的
// 交易日曆日（且台北時間 15:00 前不得寫入當日）。
func (p *Pipeline) ImportTWSEDailyAll(ctx context.Context) (ImportResult, error) {
	runID, err := p.beginIngestionRun(ctx, "twse_openapi", p.cfg.TWSEDailyAllURL, "TWSE OpenAPI", "daily_bars", nil)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{IngestionRunID: runID, Status: "running"}

	var rows []twseDailyAllRow
	checksum, err := p.fetchJSON(ctx, p.twseLimiter, p.cfg.TWSEDailyAllURL, &rows)
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}

	fallback, err := p.fallbackBarDate(ctx)
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}
	bars := parseTWSEDailyAllRows(rows, fallback, civilDateIn(p.now(), taipeiLocation()))
	imported, skipped, err := p.saveDailyBars(ctx, runID, bars)
	result.Rows = imported
	result.Skipped = skipped
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}
	dataTime := latestBarDate(bars)
	if err := p.finishIngestionRun(ctx, runID, "succeeded", imported, checksum, "", dataTime); err != nil {
		return result, err
	}
	result.Status = "succeeded"
	return result, nil
}

// ImportTPExDailyAll 匯入上櫃當日收盤行情（快照端點，僅當日，不可回補）。
func (p *Pipeline) ImportTPExDailyAll(ctx context.Context) (ImportResult, error) {
	runID, err := p.beginIngestionRun(ctx, "tpex_openapi", p.cfg.TPExDailyAllURL, "TPEx OpenAPI", "daily_bars", nil)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{IngestionRunID: runID, Status: "running"}

	var rows []tpexDailyRow
	checksum, err := p.fetchJSON(ctx, p.tpexLimiter, p.cfg.TPExDailyAllURL, &rows)
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}
	bars := parseTPExDailyRows(rows, civilDateIn(p.now(), taipeiLocation()))
	imported, skipped, err := p.saveDailyBars(ctx, runID, bars)
	result.Rows = imported
	result.Skipped = skipped
	if err != nil {
		return p.failImport(ctx, result, checksum, err)
	}
	dataTime := latestBarDate(bars)
	if err := p.finishIngestionRun(ctx, runID, "succeeded", imported, checksum, "", dataTime); err != nil {
		return result, err
	}
	result.Status = "succeeded"
	return result, nil
}

func (p *Pipeline) failImport(ctx context.Context, result ImportResult, checksum string, cause error) (ImportResult, error) {
	_ = p.finishIngestionRun(ctx, result.IngestionRunID, "failed", result.Rows, checksum, cause.Error(), nil)
	result.Status = "failed"
	result.ErrorMessage = cause.Error()
	return result, cause
}

// fallbackBarDate 回傳「最近一個已開盤的交易日曆日」：
// 台北時間 15:00 前當日資料尚未定版，不得寫入當日，往前找前一個開市日。
func (p *Pipeline) fallbackBarDate(ctx context.Context) (time.Time, error) {
	cutoff := snapshotCutoffDate(p.now())
	return p.svc.LastOpenTradingDay(ctx, "TWSE", cutoff)
}

// snapshotCutoffDate 計算快照資料允許寫入的最晚日期（純函式，可單元測試）：
// 台北時間 15:00 前最多只能寫到昨天，之後可寫到今天。
func snapshotCutoffDate(now time.Time) time.Time {
	local := now.In(taipeiLocation())
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	if local.Hour() < 15 {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// civilDateIn 取出某時刻在指定時區的日曆日（時間歸零、固定 UTC 表示）。
func civilDateIn(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// parseTWSEDailyAllRows 把 STOCK_DAY_ALL 回應標準化（純函式，可單元測試）。
// 規則：bar_date 以資料列 Date 欄為準；Date 缺漏或無法解析時用 fallback；
// 日期晚於 today（台北日曆日）的列視為異常資料丟棄。
func parseTWSEDailyAllRows(rows []twseDailyAllRow, fallback, today time.Time) []dailyBarRow {
	bars := make([]dailyBarRow, 0, len(rows))
	for _, row := range rows {
		symbol := strings.TrimSpace(row.Code)
		closePrice, ok := parseTWNumber(row.ClosingPrice)
		if symbol == "" || !ok || closePrice.LessThanOrEqual(decimal.Zero) {
			continue
		}
		barDate, err := ParseROCCompactDate(row.Date)
		if err != nil {
			barDate = fallback
		}
		if barDate.IsZero() || barDate.After(today) {
			continue
		}
		open, _ := parseTWNumber(row.OpeningPrice)
		high, _ := parseTWNumber(row.HighestPrice)
		low, _ := parseTWNumber(row.LowestPrice)
		volume, _ := parseTWNumber(row.TradeVolume)
		turnover, _ := parseTWNumber(row.TradeValue)
		bars = append(bars, dailyBarRow{
			Symbol: symbol, Name: strings.TrimSpace(row.Name), Market: "TWSE", Date: barDate,
			Open: open, High: high, Low: low, Close: closePrice, Volume: volume, Turnover: turnover,
		})
	}
	return bars
}

// parseTPExDailyRows 把 TPEx 上櫃日收盤行情標準化（純函式，可單元測試）。
// TPEx 每列都有 Date 欄；缺 Date 的列直接丟棄（上櫃快照沒有可靠的 fallback 規則）。
func parseTPExDailyRows(rows []tpexDailyRow, today time.Time) []dailyBarRow {
	bars := make([]dailyBarRow, 0, len(rows))
	for _, row := range rows {
		symbol := strings.TrimSpace(row.Code)
		closePrice, ok := parseTWNumber(row.Close)
		if symbol == "" || !ok || closePrice.LessThanOrEqual(decimal.Zero) {
			continue
		}
		barDate, err := ParseROCCompactDate(row.Date)
		if err != nil || barDate.After(today) {
			continue
		}
		open, _ := parseTWNumber(row.Open)
		high, _ := parseTWNumber(row.High)
		low, _ := parseTWNumber(row.Low)
		volume, _ := parseTWNumber(row.TradingShares)
		turnover, _ := parseTWNumber(row.TransactionAmount)
		bars = append(bars, dailyBarRow{
			Symbol: symbol, Name: strings.TrimSpace(row.Name), Market: "TPEX", Date: barDate,
			Open: open, High: high, Low: low, Close: closePrice, Volume: volume, Turnover: turnover,
		})
	}
	return bars
}

func latestBarDate(bars []dailyBarRow) *time.Time {
	var latest time.Time
	for _, bar := range bars {
		if bar.Date.After(latest) {
			latest = bar.Date
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

// saveDailyBars 寫入日 K：同（標的, 日期）已有完全相同的最新版時跳過（冪等），
// 數值不同時把舊版 is_latest 設為 false，再以新 revision 入庫（保留可追溯歷史）。
func (p *Pipeline) saveDailyBars(ctx context.Context, runID string, bars []dailyBarRow) (imported, skipped int, err error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	for _, bar := range bars {
		assetID, err := upsertAsset(ctx, tx, bar.Market, bar.Symbol, bar.Name)
		if err != nil {
			return imported, skipped, err
		}
		barDate := bar.Date.Format("2006-01-02")

		var existingOpen, existingHigh, existingLow, existingClose, existingVolume, existingTurnover *string
		err = tx.QueryRow(ctx, `
			SELECT open::text, high::text, low::text, close::text, volume_shares::text, turnover::text
			FROM market_daily_bars
			WHERE asset_id = $1 AND bar_date = $2::date AND is_latest
		`, assetID, barDate).Scan(&existingOpen, &existingHigh, &existingLow, &existingClose, &existingVolume, &existingTurnover)
		if err == nil {
			if sameDecimalText(existingOpen, bar.Open) && sameDecimalText(existingHigh, bar.High) &&
				sameDecimalText(existingLow, bar.Low) && sameDecimalText(existingClose, bar.Close) &&
				sameDecimalText(existingVolume, bar.Volume) && sameDecimalText(existingTurnover, bar.Turnover) {
				skipped++
				continue
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return imported, skipped, err
		}

		var revision int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(max(revision), -1) + 1
			FROM market_daily_bars
			WHERE asset_id = $1 AND bar_date = $2::date
		`, assetID, barDate).Scan(&revision); err != nil {
			return imported, skipped, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE market_daily_bars SET is_latest = false
			WHERE asset_id = $1 AND bar_date = $2::date
		`, assetID, barDate); err != nil {
			return imported, skipped, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO market_daily_bars (
				asset_id, bar_date, open, high, low, close, volume_shares, turnover, ingestion_run_id, revision, is_latest
			)
			VALUES ($1, $2::date, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric, $9, $10, true)
		`, assetID, barDate, bar.Open.String(), bar.High.String(), bar.Low.String(), bar.Close.String(),
			bar.Volume.String(), bar.Turnover.String(), runID, revision); err != nil {
			return imported, skipped, err
		}
		imported++
	}
	return imported, skipped, tx.Commit(ctx)
}

// sameDecimalText 比較資料庫文字數值與新值是否相等（NULL 視為 0）。
func sameDecimalText(existing *string, value decimal.Decimal) bool {
	if existing == nil {
		return value.IsZero()
	}
	d, err := decimal.NewFromString(*existing)
	if err != nil {
		return false
	}
	return d.Equal(value)
}

// upsertAsset 確保資產存在並回傳 id。台股一張固定 1000 股。
func upsertAsset(ctx context.Context, tx pgx.Tx, market, symbol, name string) (string, error) {
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
		VALUES ($1, $2, $3, $4, 'TWD', 1000)
		ON CONFLICT (market, symbol)
		DO UPDATE SET name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE assets.name END,
		              asset_type = EXCLUDED.asset_type,
		              is_active = true
		RETURNING id::text
	`, assetType, symbol, name, market).Scan(&id)
	return id, err
}

// parseTWNumber 解析 TWSE/TPEx 的數字欄位：千分位逗號、"--"（無資料）、
// 漲跌註記 "X"（除權息）都要先清掉。
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
