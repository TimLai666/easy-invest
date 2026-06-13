# Easy Invest

Easy Invest 是以台股為主、API-first 的投資建議系統。系統以交易流水推導庫存，結合市場資料與策略邏輯產生可追蹤的投資建議；預設不直接替使用者買賣。

## 文件

- `AGENTS.md`：AI agent 與後續開發規範。
- `docs/product-requirements.md`：產品需求與範圍。
- `docs/architecture.md`：系統架構。
- `docs/architecture-diagram.md`：系統架構圖。
- `docs/data-and-ledger.md`：交易流水、庫存與對帳模型。
- `docs/data-sources.md`：台股資料來源與資料新鮮度規則。
- `docs/insyra-upstream-todo.md`：Insyra 上游缺口與 TODO 紀錄。
- `docs/reference/`：外部參考文件的繁中整理版。

## 後端啟動

本專案後端已具備 Go API server、worker、PostgreSQL schema migration 與 Docker Compose。

```bash
cp .env.example .env
docker compose up -d
curl http://localhost:8080/healthz
```

Compose 會啟動：

- `api`：Go API server，對外 `http://localhost:8080`。
- `worker`：市場資料匯入與排程。
- `postgres`：PostgreSQL 16。
- `ui`：SvelteKit 靜態前端，對外 `http://localhost:3000`，並由 nginx 把 `/api` 反代到後端。

API base path 是 `/api/v1`。第一次使用可先註冊、登入，再建立 API key：

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"me@example.com","password":"passw0rd-long","display_name":"Me"}'
```

目前後端已包含：

- session 登入與 API key 管理，API key 只儲存雜湊值。
- 資產查詢、台股費稅估算、交易流水、FIFO 批次、void 後重建批次。
- `GET /portfolio` 庫存查詢與 portfolio snapshot。
- 目標權重再平衡建議引擎，保存 recommendation run/items 並附資料時間與免責聲明。
- 券商快照匯入與基本股數差異對帳。
- 回測 run 建立與查詢，重用策略核心並輸出策略、定期定額與買進持有基準。
- worker 會定期嘗試匯入 TWSE OpenAPI 當日全市場日終行情，成功或失敗都會留下 `ingestion_runs` 紀錄。

## 前端

前端位於 `web/`，使用 Svelte 5、SvelteKit 2（SPA / static adapter）、Vite 8、Tailwind v4，元件以 shadcn-svelte + bits-ui 為基礎。它是純 API 儀表板客戶端，包含登入、庫存、批次、交易事件、建議、對帳、回測、設定、API key 與市場資料頁面，每個畫面都有載入、空狀態與錯誤三態。

設計重點：

- 台股慣例**漲紅跌綠**（語意色 token），金額一律等寬數字。
- 金額/數量以 decimal 字串傳輸、用 `decimal.js` 運算，不轉浮點。
- 數量輸入一律附股／張單位選擇器；顯示同時呈現股與張。
- 交易事件只能新增或作廢，不能編輯或刪除；建議頁固定顯示免責聲明與資料時間。

開發模式（前後端分離、Vite 代理 `/api/` 到後端）：

```bash
# 視窗一：起 DB 與後端
docker compose up -d postgres
DATABASE_URL=postgres://easy_invest:easy_invest@localhost:5432/easy_invest?sslmode=disable \
  APP_SECRET=dev-secret-change-me-change-me-32bytes go run ./cmd/server

# 視窗二：起前端 dev server（http://localhost:5173）
cd web
npm.cmd install
npm.cmd run dev
```

> 註：開發時 `postgres` 需對 host 開 5432。正式部署的 compose 不對外開 DB port，前端走 `ui` 容器的 nginx 反代。

驗證與建置：

```bash
cd web
npm.cmd run check
npm.cmd run build
```

本機驗證：

```bash
go test ./...
go vet ./...
go run ./cmd/verify-marketdata
cd web && npm.cmd run check && npm.cmd run build
docker compose config
```

`verify-marketdata` 會直接連官方 TWSE/TPEx API，預設每次請求間隔 4 秒，避免驗證時對來源造成壓力。
