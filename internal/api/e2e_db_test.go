package api

// 端到端整合測試：以 httptest 起整個 chi server，跑「註冊 → 登入 → 新增交易 →
// 查庫存 → 冪等重送 → 作廢」流程。需要 TEST_DATABASE_URL 指向已跑 migration 的 DB。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tingz/easy-invest/internal/config"
)

func e2ePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("未設定 TEST_DATABASE_URL，跳過 API e2e 測試")
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
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'ledger_events')
	`).Scan(&ready); err != nil || !ready {
		pool.Close()
		t.Skip("schema 未就緒（缺 ledger_events），請先跑 migration")
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAPIEndToEndWithDatabase(t *testing.T) {
	pool := e2ePool(t)

	cfg := config.Config{
		AppSecret:          "e2e-test-secret-at-least-32-bytes!!",
		EnableRegistration: true,
		Version:            "test",
	}
	srv := NewServer(cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	base := ts.URL + "/api/v1"
	email := fmt.Sprintf("e2e-%d@example.com", time.Now().UnixNano())

	// 請求輔助：回傳狀態碼、解析後的 map、原始 header。
	do := func(method, path string, body any, headers map[string]string) (int, map[string]any, http.Header) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, base+path, reader)
		if err != nil {
			t.Fatalf("建立請求失敗：%v", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s 失敗：%v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var parsed map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &parsed)
		}
		return resp.StatusCode, parsed, resp.Header
	}

	// 1) 註冊
	status, regBody, _ := do(http.MethodPost, "/auth/register", map[string]any{
		"email": email, "password": "password1234", "display_name": "E2E",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("register 狀態 = %d, want 201（body=%v）", status, regBody)
	}
	userID, _ := regBody["id"].(string)
	if userID == "" {
		t.Fatalf("register 未回傳 user id：%v", regBody)
	}
	t.Cleanup(func() { cleanupE2EUser(pool, userID) })

	// 2) 登入（cookie 由 jar 保存）
	if status, _, _ := do(http.MethodPost, "/auth/login", map[string]any{
		"email": email, "password": "password1234",
	}, nil); status != http.StatusOK {
		t.Fatalf("login 狀態 = %d, want 200", status)
	}

	// 3) /me
	status, me, _ := do(http.MethodGet, "/me", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("/me 狀態 = %d, want 200", status)
	}
	if user, ok := me["user"].(map[string]any); !ok || user["email"] != email {
		t.Fatalf("/me email 不符：%v", me)
	}

	// 4) 入金
	if status, body, _ := do(http.MethodPost, "/ledger/events", map[string]any{
		"event_type": "cash_deposit", "trade_date": "2026-01-02", "gross_amount": "1000000",
	}, nil); status != http.StatusCreated {
		t.Fatalf("cash_deposit 狀態 = %d, want 201（body=%v）", status, body)
	}

	// 5) 買進 2330 一張（fee 留空 → 估算）
	status, buy, _ := do(http.MethodPost, "/ledger/events", map[string]any{
		"event_type": "buy", "symbol": "2330", "trade_date": "2026-01-05",
		"quantity": "1", "unit": "lot", "price": "600",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("buy 狀態 = %d, want 201（body=%v）", status, buy)
	}
	if buy["quantity_shares"] != "1000" {
		t.Fatalf("quantity_shares = %v, want 1000", buy["quantity_shares"])
	}
	if buy["fee_source"] != "estimated" {
		t.Fatalf("fee_source = %v, want estimated", buy["fee_source"])
	}
	eventID, _ := buy["id"].(string)
	if eventID == "" {
		t.Fatalf("buy 未回傳 event id：%v", buy)
	}

	// 6) 庫存應有 2330 1000 股
	status, portfolio, _ := do(http.MethodGet, "/portfolio", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("portfolio 狀態 = %d, want 200", status)
	}
	positions, _ := portfolio["positions"].([]any)
	found := false
	for _, p := range positions {
		pos, _ := p.(map[string]any)
		if pos["symbol"] == "2330" {
			found = true
			if pos["quantity_shares"] != "1000" {
				t.Fatalf("2330 quantity_shares = %v, want 1000", pos["quantity_shares"])
			}
			// 一張 = 1000 股；股/張雙顯示在庫存層應算出 1 張。
			if lots, _ := pos["quantity_lots"].(string); lots == "" {
				t.Fatalf("2330 應有 quantity_lots，實際：%v", pos["quantity_lots"])
			}
		}
	}
	if !found {
		t.Fatalf("庫存中找不到 2330：%v", positions)
	}

	// 7) 單位必填：缺 unit 應回 400 validation_failed
	status, errBody, _ := do(http.MethodPost, "/ledger/events", map[string]any{
		"event_type": "buy", "symbol": "2330", "trade_date": "2026-01-06",
		"quantity": "1", "price": "600",
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("缺 unit 狀態 = %d, want 400（body=%v）", status, errBody)
	}
	if e, ok := errBody["error"].(map[string]any); !ok || e["code"] != "validation_failed" {
		t.Fatalf("缺 unit 錯誤碼不符：%v", errBody)
	}

	// 8) 冪等：同一把 Idempotency-Key 重送，第二次帶 Idempotency-Replay 並回原結果
	idemKey := fmt.Sprintf("e2e-idem-%d", time.Now().UnixNano())
	hdr := map[string]string{"Idempotency-Key": idemKey}
	s1, b1, _ := do(http.MethodPost, "/ledger/events", map[string]any{
		"event_type": "cash_deposit", "trade_date": "2026-01-03", "gross_amount": "5000",
	}, hdr)
	if s1 != http.StatusCreated {
		t.Fatalf("第一次冪等請求狀態 = %d, want 201", s1)
	}
	s2, b2, h2 := do(http.MethodPost, "/ledger/events", map[string]any{
		"event_type": "cash_deposit", "trade_date": "2026-01-03", "gross_amount": "5000",
	}, hdr)
	if h2.Get("Idempotency-Replay") != "true" {
		t.Fatalf("第二次冪等請求應帶 Idempotency-Replay: true，實際 header = %q", h2.Get("Idempotency-Replay"))
	}
	if s2 != s1 || b1["id"] != b2["id"] {
		t.Fatalf("冪等重送應回原結果：第一次 id=%v 第二次 id=%v", b1["id"], b2["id"])
	}

	// 9) 作廢買進事件 → 204
	if status, vb, _ := do(http.MethodPost, "/ledger/events/"+eventID+"/void", map[string]any{
		"reason": "e2e 測試作廢",
	}, nil); status != http.StatusNoContent {
		t.Fatalf("void 狀態 = %d, want 204（body=%v）", status, vb)
	}
}

func cleanupE2EUser(pool *pgxpool.Pool, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stmts := []string{
		`DELETE FROM idempotency_keys WHERE user_id = $1`,
		`DELETE FROM audit_log WHERE user_id = $1`,
		`DELETE FROM lot_consumptions WHERE lot_id IN (SELECT id FROM lots WHERE user_id = $1)`,
		`DELETE FROM lots WHERE user_id = $1`,
		`DELETE FROM portfolio_snapshots WHERE user_id = $1`,
		`DELETE FROM ledger_events WHERE user_id = $1`,
		`DELETE FROM api_keys WHERE user_id = $1`,
		`DELETE FROM user_settings WHERE user_id = $1`,
		`DELETE FROM users WHERE id = $1`,
	}
	for _, stmt := range stmts {
		_, _ = pool.Exec(ctx, stmt, userID)
	}
}
