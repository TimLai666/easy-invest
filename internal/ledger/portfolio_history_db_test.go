package ledger

// DB 整合測試：需要 TEST_DATABASE_URL 指向已跑完 migration 且已匯入真實市場資料的 PostgreSQL。
// 沒有資料庫或沒有 0050 真實日 K 時直接 t.Skip，避免用假行情取代 M3 驗收。

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func portfolioHistoryTestPool(t *testing.T) *pgxpool.Pool {
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

func TestPortfolioHistoryWithRealMarketData(t *testing.T) {
	pool := portfolioHistoryTestPool(t)
	ctx := context.Background()

	var firstBarDate, firstClose string
	err := pool.QueryRow(ctx, `
		SELECT b.bar_date::text, b.close::text
		FROM market_daily_bars b
		JOIN assets a ON a.id = b.asset_id
		WHERE a.symbol = '0050'
		  AND a.market = 'TWSE'
		  AND b.is_latest
		  AND b.bar_date BETWEEN '2024-01-02'::date AND '2024-01-05'::date
		ORDER BY b.bar_date
		LIMIT 1
	`).Scan(&firstBarDate, &firstClose)
	if err != nil {
		if err == pgx.ErrNoRows {
			t.Skip("測試資料庫尚未匯入 0050 於 2024-01 的真實日 K")
		}
		t.Fatalf("讀取 0050 真實日 K 失敗：%v", err)
	}

	var hasCalendar bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM trading_calendar
			WHERE market = 'TWSE'
			  AND is_open
			  AND cal_date = $1::date
		)
	`, firstBarDate).Scan(&hasCalendar); err != nil {
		t.Fatalf("讀取交易日曆失敗：%v", err)
	}
	if !hasCalendar {
		t.Skipf("測試資料庫尚未匯入 %s 的 TWSE 交易日曆", firstBarDate)
	}

	userID := setupPortfolioHistoryUser(t, ctx, pool)
	svc := NewService(pool)
	deposit := decimal.NewFromInt(1000000)
	closePrice, err := decimal.NewFromString(firstClose)
	if err != nil {
		t.Fatalf("真實收盤價格式錯誤：%v", err)
	}

	if _, err := svc.CreateEvent(ctx, CreateEventInput{
		UserID:      userID,
		EventType:   EventCashDeposit,
		TradeDate:   firstBarDate,
		GrossAmount: stringPtr(deposit.String()),
	}); err != nil {
		t.Fatalf("建立入金事件失敗：%v", err)
	}
	if _, err := svc.CreateEvent(ctx, CreateEventInput{
		UserID:    userID,
		EventType: EventBuy,
		Symbol:    "0050",
		TradeDate: firstBarDate,
		Quantity:  "1000",
		Unit:      "share",
		Price:     closePrice.String(),
		Fee:       stringPtr("0"),
		Tax:       stringPtr("0"),
	}); err != nil {
		t.Fatalf("建立買進事件失敗：%v", err)
	}

	points, err := svc.PortfolioHistory(ctx, userID, firstBarDate, "2024-01-05", 10)
	if err != nil {
		t.Fatalf("讀取 portfolio history 失敗：%v", err)
	}
	if len(points) == 0 {
		t.Fatal("portfolio history 不應為空")
	}
	first := points[0]
	if first.Date != firstBarDate {
		t.Fatalf("第一筆日期 = %s，want %s", first.Date, firstBarDate)
	}
	if !first.IsComplete || len(first.MissingPriceSymbols) != 0 {
		t.Fatalf("第一筆應有完整真實行情，got complete=%v missing=%v", first.IsComplete, first.MissingPriceSymbols)
	}
	if first.MarketDataAsOf == nil || *first.MarketDataAsOf != firstBarDate {
		t.Fatalf("market_data_as_of = %v，want %s", first.MarketDataAsOf, firstBarDate)
	}
	total, err := decimal.NewFromString(first.TotalValue)
	if err != nil {
		t.Fatalf("total_value 格式錯誤：%v", err)
	}
	if !total.Equal(deposit) {
		t.Fatalf("第一天總資產 = %s，want %s", total, deposit)
	}
}

func setupPortfolioHistoryUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	email := fmt.Sprintf("portfolio-history-test-%d@example.com", time.Now().UnixNano())
	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash) VALUES ($1, 'test-only') RETURNING id::text
	`, email).Scan(&userID); err != nil {
		t.Fatalf("建立測試使用者失敗：%v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_settings (user_id) VALUES ($1)`, userID); err != nil {
		t.Fatalf("建立測試使用者設定失敗：%v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		statements := []string{
			`DELETE FROM lot_consumptions WHERE lot_id IN (SELECT id FROM lots WHERE user_id = $1)`,
			`DELETE FROM lots WHERE user_id = $1`,
			`DELETE FROM portfolio_snapshots WHERE user_id = $1`,
			`DELETE FROM ledger_events WHERE user_id = $1`,
			`DELETE FROM user_settings WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		}
		for _, stmt := range statements {
			if _, err := pool.Exec(cleanupCtx, stmt, userID); err != nil {
				t.Logf("清理失敗（%s）：%v", stmt, err)
			}
		}
	})
	return userID
}

func stringPtr(value string) *string { return &value }
