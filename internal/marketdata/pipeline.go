package marketdata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSourceBlocked 表示來源回應 429/403 或內容異常（HTML 錯誤頁），呼叫端應退避而非重試。
var ErrSourceBlocked = errors.New("來源拒絕請求或回應異常，進入退避")

// PipelineConfig 是市場資料管線的來源端點與抓取參數。
// 端點預設值以 docs/data-sources.md 的實測結果為準。
type PipelineConfig struct {
	TWSEDailyAllURL     string // 上市當日全市場日成交（OpenAPI STOCK_DAY_ALL）
	TWSEStockDayURL     string // 上市個股月度歷史（STOCK_DAY，按月回補用）
	TWSECorpActionsURL  string // 上市除權除息預告表（OpenAPI TWT48U_ALL）
	TWSEHolidayQueryURL string // 市場開休市日期查詢（rwd 版，可指定年度）
	TPExDailyAllURL     string // 上櫃當日收盤行情（TPEx OpenAPI）

	// BackfillMonths 是沒有交易紀錄可參考時（關注清單、基準標的）的歷史回補窗口；
	// 持有過的標的會從最早交易日再往前推這個窗口開始補。
	BackfillMonths int
	// RequestInterval 是同一來源兩個請求之間的最小間隔（再加 0〜RequestJitter 隨機值）。
	RequestInterval time.Duration
	RequestJitter   time.Duration
	UserAgent       string
}

// DefaultPipelineConfig 回傳實測過的預設端點與保守抓取參數。
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		TWSEDailyAllURL:     "https://openapi.twse.com.tw/v1/exchangeReport/STOCK_DAY_ALL",
		TWSEStockDayURL:     "https://www.twse.com.tw/exchangeReport/STOCK_DAY",
		TWSECorpActionsURL:  "https://openapi.twse.com.tw/v1/exchangeReport/TWT48U_ALL",
		TWSEHolidayQueryURL: "https://www.twse.com.tw/rwd/zh/holidaySchedule/holidaySchedule",
		TPExDailyAllURL:     "https://www.tpex.org.tw/openapi/v1/tpex_mainboard_daily_close_quotes",
		BackfillMonths:      24,
		RequestInterval:     4 * time.Second,
		RequestJitter:       1 * time.Second,
		UserAgent:           "easy-invest/1.0",
	}
}

// Pipeline 負責對外抓取與入庫；讀取查詢留在 Service。
// 策略核心不碰這裡——資料先入庫，再由外層整理成策略輸入。
type Pipeline struct {
	svc  *Service
	db   *pgxpool.Pool
	cfg  PipelineConfig
	log  *slog.Logger
	http *http.Client

	twseLimiter *SourceLimiter
	tpexLimiter *SourceLimiter

	now func() time.Time // 測試時可替換
}

// NewPipeline 建立市場資料管線。log 可為 nil（改用預設 logger）。
func NewPipeline(db *pgxpool.Pool, cfg PipelineConfig, log *slog.Logger) *Pipeline {
	if log == nil {
		log = slog.Default()
	}
	if cfg.RequestInterval <= 0 {
		cfg.RequestInterval = 4 * time.Second
	}
	if cfg.BackfillMonths <= 0 {
		cfg.BackfillMonths = 24
	}
	return &Pipeline{
		svc:         NewService(db),
		db:          db,
		cfg:         cfg,
		log:         log,
		http:        &http.Client{Timeout: 60 * time.Second},
		twseLimiter: NewSourceLimiter("twse", cfg.RequestInterval, cfg.RequestJitter),
		tpexLimiter: NewSourceLimiter("tpex", cfg.RequestInterval, cfg.RequestJitter),
		now:         time.Now,
	}
}

// Service 回傳底層的讀取服務（給 worker 查交易日曆等）。
func (p *Pipeline) Service() *Service { return p.svc }

// fetchJSON 對來源發出 GET 並解析 JSON：
//   - 先過該來源的全域 rate limiter；
//   - 帶誠實 User-Agent，不偽裝瀏覽器；
//   - 429/403 或回應內容是 HTML 錯誤頁時回傳 ErrSourceBlocked 並觸發指數退避。
//
// 回傳原始回應的 sha256 checksum，供 ingestion_runs 記錄。
func (p *Pipeline) fetchJSON(ctx context.Context, limiter *SourceLimiter, url string, out any) (string, error) {
	if err := limiter.Wait(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		limiter.ReportFailure()
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		fails := limiter.ReportFailure()
		return "", fmt.Errorf("%w: %s 回應 %d（連續失敗 %d 次）", ErrSourceBlocked, url, resp.StatusCode, fails)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limiter.ReportFailure()
		return "", fmt.Errorf("來源回應狀態碼 %d: %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		limiter.ReportFailure()
		return "", err
	}
	if looksLikeHTML(body) {
		fails := limiter.ReportFailure()
		return "", fmt.Errorf("%w: %s 回應內容是 HTML 錯誤頁（連續失敗 %d 次）", ErrSourceBlocked, url, fails)
	}
	if err := json.Unmarshal(body, out); err != nil {
		limiter.ReportFailure()
		return "", fmt.Errorf("解析 %s 回應失敗: %w", url, err)
	}
	limiter.ReportSuccess()
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// looksLikeHTML 判斷回應內容是否為 HTML 錯誤頁（被擋時常見）。
func looksLikeHTML(body []byte) bool {
	trimmed := bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF}) // 去掉 UTF-8 BOM
	trimmed = bytes.TrimLeft(trimmed, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	lower := strings.ToLower(string(trimmed[:min(64, len(trimmed))]))
	return strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html")
}

// beginIngestionRun 建立一筆 ingestion_runs 紀錄並回傳 id。
// dataset 名稱固定使用：daily_bars、daily_bars_backfill、corporate_actions、trading_calendar、securities_list。
func (p *Pipeline) beginIngestionRun(ctx context.Context, sourceName, sourceURL, sourceLicense, dataset string, dataTime *time.Time) (string, error) {
	var runID string
	err := p.db.QueryRow(ctx, `
		INSERT INTO ingestion_runs (source_name, source_url, source_license, dataset, fetched_at, data_time, status)
		VALUES ($1, $2, $3, $4, now(), $5, 'running')
		RETURNING id::text
	`, sourceName, sourceURL, sourceLicense, dataset, dataTime).Scan(&runID)
	return runID, err
}

// finishIngestionRun 更新 ingestion_runs 的最終狀態。
// ingestion_runs 是匯入流程的執行紀錄，不是 ledger 業務事件，允許 UPDATE。
func (p *Pipeline) finishIngestionRun(ctx context.Context, runID, status string, rowCount int, checksum, errorMessage string, dataTime *time.Time) error {
	_, err := p.db.Exec(ctx, `
		UPDATE ingestion_runs
		SET status = $2,
		    row_count = $3,
		    checksum = NULLIF($4, ''),
		    error_message = NULLIF($5, ''),
		    data_time = COALESCE($6, data_time)
		WHERE id = $1
	`, runID, status, rowCount, checksum, errorMessage, dataTime)
	return err
}
