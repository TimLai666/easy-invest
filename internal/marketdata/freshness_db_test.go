package marketdata

// DB 整合測試：需要 TEST_DATABASE_URL 指向已跑完 migration 的 PostgreSQL。
// 以獨一無二的 market 代碼隔離資料，讓斷言在共用 dev DB 上仍可重現。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("未設定 TEST_DATABASE_URL，跳過 DB 整合測試")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("無法建立連線池：%v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("資料庫連不上：%v", err)
	}
	var ready bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'market_daily_bars')
	`).Scan(&ready); err != nil || !ready {
		pool.Close()
		t.Skip("schema 未就緒（缺 market_daily_bars），請先跑 migration")
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedMarket 建立一個獨立 market 的交易日曆、資產、匯入紀錄與一筆日 K，回傳 (market, dataset, assetID, runID)。
func seedMarket(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	nano := time.Now().UnixNano()
	market := fmt.Sprintf("EITEST_%d", nano)
	dataset := fmt.Sprintf("eitest_ds_%d", nano)

	// 交易日曆：(06 開, 07 開, 08 休, 09 開, 10 開)
	cal := []struct {
		date string
		open bool
	}{
		{"2031-01-06", true}, {"2031-01-07", true}, {"2031-01-08", false},
		{"2031-01-09", true}, {"2031-01-10", true},
	}
	for _, c := range cal {
		if _, err := pool.Exec(ctx, `
			INSERT INTO trading_calendar (market, cal_date, is_open) VALUES ($1, $2::date, $3)
			ON CONFLICT (market, cal_date) DO UPDATE SET is_open = EXCLUDED.is_open
		`, market, c.date, c.open); err != nil {
			t.Fatalf("建立交易日曆失敗：%v", err)
		}
	}

	var runID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO ingestion_runs (source_name, source_url, source_license, dataset, fetched_at, data_time, status, row_count)
		VALUES ('eitest', 'local', 'test', $1, now(), '2031-01-09T08:00:00Z', 'succeeded', 1)
		RETURNING id::text
	`, dataset).Scan(&runID); err != nil {
		t.Fatalf("建立匯入紀錄失敗：%v", err)
	}

	var assetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO assets (asset_type, symbol, name, market, currency, lot_size)
		VALUES ('tw_stock', $1, '測試股', $2, 'TWD', 1000)
		RETURNING id::text
	`, fmt.Sprintf("T%d", nano%100000), market).Scan(&assetID); err != nil {
		t.Fatalf("建立測試資產失敗：%v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO market_daily_bars (asset_id, bar_date, open, high, low, close, ingestion_run_id, revision, is_latest)
		VALUES ($1, '2031-01-09'::date, 100, 100, 100, 100, $2, 0, true)
	`, assetID, runID); err != nil {
		t.Fatalf("建立日 K 失敗：%v", err)
	}

	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, stmt := range []string{
			`DELETE FROM market_daily_bars WHERE asset_id = $1`,
			`DELETE FROM assets WHERE id = $1`,
		} {
			if _, err := pool.Exec(c, stmt, assetID); err != nil {
				t.Logf("清理失敗（%s）：%v", stmt, err)
			}
		}
		if _, err := pool.Exec(c, `DELETE FROM ingestion_runs WHERE id = $1`, runID); err != nil {
			t.Logf("清理匯入紀錄失敗：%v", err)
		}
		if _, err := pool.Exec(c, `DELETE FROM trading_calendar WHERE market = $1`, market); err != nil {
			t.Logf("清理交易日曆失敗：%v", err)
		}
	})
	return market, dataset, assetID
}

func TestCountOpenDaysBetweenWithDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	market, _, _ := seedMarket(t, ctx, pool)
	svc := NewService(pool)

	// (06, 10] 內開市日：07、09、10 = 3。
	got, err := svc.countOpenDaysBetween(ctx, market, mustDate("2031-01-06"), mustDate("2031-01-10"))
	if err != nil {
		t.Fatalf("countOpenDaysBetween: %v", err)
	}
	if got != 3 {
		t.Fatalf("開市日數 = %d, want 3", got)
	}
}

func TestLastOpenTradingDayWithDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	market, _, _ := seedMarket(t, ctx, pool)
	svc := NewService(pool)

	// 01-08 為休市，往前最近開市日是 01-07。
	got, err := svc.LastOpenTradingDay(ctx, market, mustDate("2031-01-08"))
	if err != nil {
		t.Fatalf("LastOpenTradingDay: %v", err)
	}
	if got.Format("2006-01-02") != "2031-01-07" {
		t.Fatalf("最近開市日 = %s, want 2031-01-07", got.Format("2006-01-02"))
	}
}

func TestStaleTradingDaysWithDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	market, _, _ := seedMarket(t, ctx, pool)
	svc := NewService(pool)

	// 有資料：最新資料日應為 2031-01-09，落後天數非負。
	lag, latest, err := svc.StaleTradingDays(ctx, market)
	if err != nil {
		t.Fatalf("StaleTradingDays: %v", err)
	}
	if latest.Format("2006-01-02") != "2031-01-09" {
		t.Fatalf("最新資料日 = %s, want 2031-01-09", latest.Format("2006-01-02"))
	}
	if lag < 0 {
		t.Fatalf("落後天數 = %d, 不應為負", lag)
	}

	// 沒有任何 bars 的 market 應回 ErrNoMarketData。
	if _, _, err := svc.StaleTradingDays(ctx, market+"_EMPTY"); !errors.Is(err, ErrNoMarketData) {
		t.Fatalf("空 market 應回 ErrNoMarketData，實際：%v", err)
	}
}

func TestFreshnessWithDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, dataset, _ := seedMarket(t, ctx, pool)
	svc := NewService(pool)

	items, err := svc.Freshness(ctx)
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	var mine map[string]any
	for _, it := range items {
		if it["dataset"] == dataset {
			mine = it
			break
		}
	}
	if mine == nil {
		t.Fatalf("Freshness 結果應包含 dataset %s", dataset)
	}
	if mine["status"] != "succeeded" {
		t.Fatalf("status = %v, want succeeded", mine["status"])
	}
	if mine["source_name"] != "eitest" {
		t.Fatalf("source_name = %v, want eitest", mine["source_name"])
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}
