package reconcile

// DB 整合測試：需要 TEST_DATABASE_URL 指向一個已跑完 migration 的 PostgreSQL。
// 連不上或 schema 未就緒時直接 t.Skip，不影響純邏輯測試。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/ledger"
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
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'reconciliation_diffs')
	`).Scan(&ready); err != nil || !ready {
		pool.Close()
		t.Skip("schema 未就緒（缺 reconciliation_diffs），請先跑 migration")
	}
	t.Cleanup(pool.Close)
	return pool
}

// setupTestUser 建立測試使用者與設定，並註冊清理（依外鍵順序倒著刪）。
func setupTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	email := fmt.Sprintf("reconcile-test-%d@example.com", time.Now().UnixNano())
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
			`DELETE FROM reconciliation_diffs WHERE run_id IN (SELECT id FROM reconciliation_runs WHERE user_id = $1)`,
			`DELETE FROM reconciliation_runs WHERE user_id = $1`,
			`DELETE FROM broker_positions WHERE snapshot_id IN (SELECT id FROM broker_snapshots WHERE user_id = $1)`,
			`DELETE FROM broker_snapshots WHERE user_id = $1`,
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

func mustCreateEvent(t *testing.T, ctx context.Context, svc *ledger.Service, input ledger.CreateEventInput) ledger.Event {
	t.Helper()
	event, err := svc.CreateEvent(ctx, input)
	if err != nil {
		t.Fatalf("建立 ledger 事件失敗（%s %s）：%v", input.EventType, input.Symbol, err)
	}
	return event
}

func strPtr(value string) *string { return &value }

func TestReconcileFlowWithDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := setupTestUser(t, ctx, pool)

	ledgerSvc := ledger.NewService(pool)
	svc := NewService(pool, ledgerSvc)

	// 內部帳：入金 + 買 2330 一張 + 買 0050 一張。
	mustCreateEvent(t, ctx, ledgerSvc, ledger.CreateEventInput{
		UserID: userID, EventType: ledger.EventCashDeposit, TradeDate: "2026-01-02",
		GrossAmount: strPtr("10000000"),
	})
	mustCreateEvent(t, ctx, ledgerSvc, ledger.CreateEventInput{
		UserID: userID, EventType: ledger.EventBuy, Symbol: "2330", TradeDate: "2026-01-05",
		Quantity: "1000", Unit: "share", Price: "600", Fee: strPtr("855"), Tax: strPtr("0"),
	})
	mustCreateEvent(t, ctx, ledgerSvc, ledger.CreateEventInput{
		UserID: userID, EventType: ledger.EventBuy, Symbol: "0050", TradeDate: "2026-01-05",
		Quantity: "1000", Unit: "share", Price: "100", Fee: strPtr("143"), Tax: strPtr("0"),
	})

	// 公司行動：2330 現金股利，除息日落在持有期間，ledger 沒有對應 cash_dividend → 應列漏記。
	var actionID, asset2330 string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM assets WHERE symbol = '2330' AND is_active = true LIMIT 1`).Scan(&asset2330); err != nil {
		t.Fatalf("找不到種子資產 2330：%v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO corporate_actions (asset_id, action_type, ex_date, pay_date, cash_per_share)
		VALUES ($1, 'cash_dividend', '2026-03-10', '2026-04-15', 5)
		ON CONFLICT (asset_id, action_type, ex_date)
		DO UPDATE SET pay_date = EXCLUDED.pay_date, cash_per_share = EXCLUDED.cash_per_share
		RETURNING id::text
	`, asset2330).Scan(&actionID); err != nil {
		t.Fatalf("建立公司行動失敗：%v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM corporate_actions WHERE id = $1`, actionID); err != nil {
			t.Logf("清理公司行動失敗：%v", err)
		}
	})

	// 券商快照：2330 股數與均價都不同且帶費稅欄位、00878 內部沒有、0050 缺漏。
	snapshot, err := svc.CreateBrokerSnapshot(ctx, CreateSnapshotInput{
		UserID:     userID,
		BrokerName: "永豐金",
		Positions: []BrokerPositionInput{
			{Symbol: "2330", QuantityShares: "2000", BrokerAvgCost: strPtr("999.5"), Details: map[string]any{"total_fee": "999"}},
			{Symbol: "00878", QuantityShares: "500"},
		},
	})
	if err != nil {
		t.Fatalf("建立券商快照失敗：%v", err)
	}

	run, err := svc.CreateRun(ctx, userID, snapshot.ID)
	if err != nil {
		t.Fatalf("建立對帳失敗：%v", err)
	}
	if run.Status != "open" {
		t.Fatalf("run.Status = %q, want open", run.Status)
	}

	// 預期差異（同一個 dev DB 可能殘留其他公司行動，所以檢查「至少包含」這幾筆）。
	type diffKey struct{ diffType, symbol string }
	found := map[diffKey]Diff{}
	for _, diff := range run.Diffs {
		symbol := ""
		if diff.Symbol != nil {
			symbol = *diff.Symbol
		}
		found[diffKey{diff.DiffType, symbol}] = diff
	}
	expectations := []struct {
		key      diffKey
		internal string
		broker   string
	}{
		{diffKey{DiffQuantityMismatch, "2330"}, "1000", "2000"},
		{diffKey{DiffAvgCostMismatch, "2330"}, "600.855", "999.5"},
		{diffKey{DiffFeeTaxMismatch, "2330"}, "855", "999"},
		{diffKey{DiffMissingPositionInternal, "00878"}, "", "500"},
		{diffKey{DiffMissingPositionBroker, "0050"}, "1000", ""},
		{diffKey{DiffMissingDividend, "2330"}, "", "5000"},
	}
	for _, want := range expectations {
		diff, ok := found[want.key]
		if !ok {
			t.Fatalf("缺少預期差異 %+v，實際：%+v", want.key, run.Diffs)
		}
		if want.internal != "" && (diff.InternalValue == nil || *diff.InternalValue != want.internal) {
			t.Fatalf("%+v internal_value = %v, want %s", want.key, diff.InternalValue, want.internal)
		}
		if want.broker != "" && (diff.BrokerValue == nil || *diff.BrokerValue != want.broker) {
			t.Fatalf("%+v broker_value = %v, want %s", want.key, diff.BrokerValue, want.broker)
		}
	}

	// resolution=adjusted 處理漏記配息：應建立 cash_dividend 事件並回填關聯。
	dividendDiff := found[diffKey{DiffMissingDividend, "2330"}]
	resolved, err := svc.ResolveDiffWithInput(ctx, userID, dividendDiff.ID, ResolveDiffInput{Resolution: "adjusted"})
	if err != nil {
		t.Fatalf("resolve 漏記配息失敗：%v", err)
	}
	if resolved.Resolution != "adjusted" || resolved.AdjustmentEventID == nil || resolved.ResolvedAt == nil {
		t.Fatalf("resolve 結果不完整：%+v", resolved)
	}
	var eventType, source, gross, tradeDate string
	var runIDOnEvent *string
	if err := pool.QueryRow(ctx, `
		SELECT event_type, source, gross_amount::text, trade_date::text, reconciliation_run_id::text
		FROM ledger_events WHERE id = $1 AND user_id = $2
	`, *resolved.AdjustmentEventID, userID).Scan(&eventType, &source, &gross, &tradeDate, &runIDOnEvent); err != nil {
		t.Fatalf("讀取調整事件失敗：%v", err)
	}
	if eventType != ledger.EventCashDividend {
		t.Fatalf("event_type = %q, want cash_dividend", eventType)
	}
	if source != "reconciliation" {
		t.Fatalf("source = %q, want reconciliation", source)
	}
	if !decimal.RequireFromString(gross).Equal(decimal.NewFromInt(5000)) {
		t.Fatalf("gross_amount = %q, want 5000", gross)
	}
	if tradeDate != "2026-04-15" {
		t.Fatalf("trade_date = %q, want 配息日 2026-04-15", tradeDate)
	}
	if runIDOnEvent == nil || *runIDOnEvent != run.ID {
		t.Fatalf("reconciliation_run_id = %v, want %s", runIDOnEvent, run.ID)
	}

	// 已處理過的差異不可重複 resolve。
	if _, err := svc.ResolveDiff(ctx, userID, dividendDiff.ID, "accepted_as_is"); err == nil {
		t.Fatal("重複 resolve 應該要報錯")
	}

	// 其餘差異全部標記 accepted_as_is 後，run 應自動轉 resolved。
	current, err := svc.GetRun(ctx, userID, run.ID)
	if err != nil {
		t.Fatalf("讀取 run 失敗：%v", err)
	}
	for _, diff := range current.Diffs {
		if diff.Resolution != "pending" {
			continue
		}
		if _, err := svc.ResolveDiff(ctx, userID, diff.ID, "accepted_as_is"); err != nil {
			t.Fatalf("resolve 差異 %s 失敗：%v", diff.ID, err)
		}
	}
	final, err := svc.GetRun(ctx, userID, run.ID)
	if err != nil {
		t.Fatalf("讀取 run 失敗：%v", err)
	}
	if final.Status != "resolved" {
		t.Fatalf("全部差異處理完後 run.Status = %q, want resolved", final.Status)
	}
}

func TestCreateBrokerSnapshotFromCSVWithDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := setupTestUser(t, ctx, pool)

	svc := NewService(pool, ledger.NewService(pool))
	csv := "\xEF\xBB\xBFsymbol,quantity_shares,avg_cost\n2330,1000,600.86\n"
	snapshot, err := svc.CreateBrokerSnapshotFromCSV(ctx, userID, "永豐金", "generic", nil, strings.NewReader(csv))
	if err != nil {
		t.Fatalf("CSV 匯入失敗：%v", err)
	}
	if snapshot.Source != "csv_import" {
		t.Fatalf("source = %q, want csv_import", snapshot.Source)
	}
	var count int
	var rawCSV string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), (SELECT raw_payload->>'csv' FROM broker_snapshots WHERE id = $1)
		FROM broker_positions WHERE snapshot_id = $1
	`, snapshot.ID).Scan(&count, &rawCSV); err != nil {
		t.Fatalf("讀取快照部位失敗：%v", err)
	}
	if count != 1 {
		t.Fatalf("broker_positions 筆數 = %d, want 1", count)
	}
	if !strings.Contains(rawCSV, "2330,1000,600.86") {
		t.Fatalf("raw_payload 應保留原始 CSV，實際：%q", rawCSV)
	}
}
