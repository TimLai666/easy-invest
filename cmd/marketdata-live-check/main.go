package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tingz/easy-invest/internal/config"
	"github.com/tingz/easy-invest/internal/marketdata"
	"github.com/tingz/easy-invest/internal/platform"
)

type sampleBar struct {
	Symbol   string `json:"symbol"`
	Market   string `json:"market"`
	BarDate  string `json:"bar_date"`
	Close    string `json:"close"`
	Volume   string `json:"volume_shares"`
	Turnover string `json:"turnover"`
}

type tableCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func main() {
	backfillMonths := flag.Int("backfill-months", 2, "RunCatchUp 使用的歷史回補窗口（月）")
	requestInterval := flag.Duration("request-interval", 4*time.Second, "同一官方來源兩次請求之間的最小間隔")
	requestJitter := flag.Duration("request-jitter", 0, "請求間隔隨機 jitter；驗證預設 0 以利重現")
	sampleMonthText := flag.String("sample-month", "2024-01-01", "額外驗證 TWSE/TPEx 歷史月回補的月份第一天")
	twseSample := flag.String("twse-sample", "0050", "額外驗證的 TWSE 樣本代號")
	tpexSample := flag.String("tpex-sample", "006201", "額外驗證的 TPEx 樣本代號")
	skipCatchUp := flag.Bool("skip-catchup", false, "只做樣本歷史月回補與查詢，不跑完整 catch-up")
	flag.Parse()

	if *backfillMonths <= 0 {
		fail(fmt.Errorf("backfill-months 必須大於 0"))
	}
	sampleMonth, err := time.Parse("2006-01-02", *sampleMonthText)
	if err != nil {
		fail(fmt.Errorf("sample-month 格式必須是 YYYY-MM-DD: %w", err))
	}

	cfg := config.Load()
	pipelineCfg := marketdata.DefaultPipelineConfig()
	pipelineCfg.TWSEDailyAllURL = cfg.TWSEDailyAllURL
	pipelineCfg.TWSEStockDayURL = cfg.TWSEStockDayURL
	pipelineCfg.TWSECorpActionsURL = cfg.TWSECorpActionsURL
	pipelineCfg.TWSEHolidayQueryURL = cfg.TWSEHolidayQueryURL
	pipelineCfg.TPExDailyAllURL = cfg.TPExDailyAllURL
	pipelineCfg.TPExStockDayURL = cfg.TPExStockDayURL
	pipelineCfg.BackfillMonths = *backfillMonths
	pipelineCfg.RequestInterval = *requestInterval
	pipelineCfg.RequestJitter = *requestJitter

	ctx := context.Background()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := platform.RunMigrations(ctx, cfg.DatabaseURL); err != nil {
		fail(fmt.Errorf("migration 失敗: %w", err))
	}
	db, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fail(fmt.Errorf("資料庫連線失敗: %w", err))
	}
	defer db.Close()

	pipeline := marketdata.NewPipeline(db, pipelineCfg, log)
	if result, err := pipeline.SyncSecuritiesList(ctx); err != nil {
		fail(fmt.Errorf("同步上市櫃證券清單失敗（run_id=%s）: %w", result.IngestionRunID, err))
	}

	if !*skipCatchUp {
		if err := pipeline.RunCatchUp(ctx); err != nil {
			fail(fmt.Errorf("catch-up 失敗: %w", err))
		}
	}

	twseRows, err := pipeline.BackfillSymbolMonth(ctx, marketdata.TrackedSymbol{Symbol: *twseSample, Market: "TWSE"}, sampleMonth)
	if err != nil {
		fail(fmt.Errorf("TWSE 樣本歷史回補失敗: %w", err))
	}
	tpexRows, err := pipeline.BackfillSymbolMonth(ctx, marketdata.TrackedSymbol{Symbol: *tpexSample, Market: "TPEX"}, sampleMonth)
	if err != nil {
		fail(fmt.Errorf("TPEx 樣本歷史回補失敗: %w", err))
	}

	service := marketdata.NewService(db)
	freshness, err := service.Freshness(ctx)
	if err != nil {
		fail(fmt.Errorf("讀取 freshness 失敗: %w", err))
	}
	counts, err := readCounts(ctx, db)
	if err != nil {
		fail(err)
	}
	samples, err := readSampleBars(ctx, db, []string{*twseSample, *tpexSample}, sampleMonth)
	if err != nil {
		fail(err)
	}
	if len(samples) < 2 {
		fail(fmt.Errorf("樣本行情不足，got %d want >= 2", len(samples)))
	}

	out := map[string]any{
		"checked_at":            time.Now().Format(time.RFC3339),
		"database_url_redacted": redactDatabaseURL(cfg.DatabaseURL),
		"request_interval":      requestInterval.String(),
		"backfill_months":       *backfillMonths,
		"sample_month":          sampleMonth.Format("2006-01-02"),
		"sample_imported_rows": map[string]int{
			*twseSample: twseRows,
			*tpexSample: tpexRows,
		},
		"counts":    counts,
		"samples":   samples,
		"freshness": freshness,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
}

func readCounts(ctx context.Context, db *pgxpool.Pool) ([]tableCount, error) {
	tables := []string{"assets", "ingestion_runs", "trading_calendar", "market_daily_bars", "corporate_actions", "backfill_checkpoints"}
	counts := make([]tableCount, 0, len(tables))
	for _, table := range tables {
		var count int64
		query := fmt.Sprintf("SELECT count(*) FROM %s", table)
		if err := db.QueryRow(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("計算 %s 筆數失敗: %w", table, err)
		}
		counts = append(counts, tableCount{Name: table, Count: count})
	}
	return counts, nil
}

func readSampleBars(ctx context.Context, db *pgxpool.Pool, symbols []string, month time.Time) ([]sampleBar, error) {
	rows, err := db.Query(ctx, `
		SELECT a.symbol, a.market, b.bar_date::text, b.close::text, b.volume_shares::text, b.turnover::text
		FROM market_daily_bars b
		JOIN assets a ON a.id = b.asset_id
		WHERE b.is_latest
		  AND a.symbol = ANY($1)
		  AND b.bar_date >= $2::date
		  AND b.bar_date < ($2::date + INTERVAL '1 month')
		ORDER BY a.symbol, b.bar_date
	`, symbols, month.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("讀取樣本行情失敗: %w", err)
	}
	defer rows.Close()
	var samples []sampleBar
	for rows.Next() {
		var item sampleBar
		if err := rows.Scan(&item.Symbol, &item.Market, &item.BarDate, &item.Close, &item.Volume, &item.Turnover); err != nil {
			return nil, err
		}
		samples = append(samples, item)
	}
	return samples, rows.Err()
}

func redactDatabaseURL(value string) string {
	at := strings.LastIndex(value, "@")
	scheme := strings.Index(value, "://")
	if at < 0 || scheme < 0 || scheme > at {
		return value
	}
	return value[:scheme+3] + "***:***" + value[at:]
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
