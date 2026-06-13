# 任務交接：Easy Invest 台股投資建議系統——完整迭代到可用

你接手一個進行中的專案。**動工前先依序讀完下列文件與本 prompt，再開始。** 所有回覆、程式註解、commit message、UI 文案一律用**台灣繁體中文**，避免中國用語與翻譯腔。交付直接給完成版，不要結尾追問。

## 0. 專案定位

Easy Invest 是以台股為主、**API-first** 的投資建議系統。依使用者庫存、交易流水、配息與市場資料，產生**可追蹤、可解釋**的建議（賣哪些、買哪些、調到什麼目標比例）。**只提供建議，不替使用者下單。** 後端 Go，DB PostgreSQL，UI 是 API 的儀表板客戶端，部署用 Docker Compose。

## 1. 文件真源（先讀，衝突時以這些為準）

位於 `docs/`：
- `product-requirements.md`、`architecture.md`、`architecture-diagram.md`
- `data-and-ledger.md`、`data-sources.md`（資料來源優先序、catch-up、rate limit）
- `implementation-plan.md`（**里程碑 M0–M6 與驗收條件，最重要**）
- `db-schema.md`（schema 與 migration 紀律）
- `api-spec.md` + `api/openapi.yaml`（API 合約；機器可讀真源是 openapi.yaml）
- `tw-market-rules.md`（**台股費稅、公司行動、捨入規則 + 五個驗收算例**）
- `insyra-upstream-todo.md`（Insyra 使用原則）

`AGENTS.md` / `CLAUDE.md` 是專案憲法，務必遵守。`docs/reference/` 只是靈感，不是規格。

## 2. 技術選型（已定案，不要改）

- 後端：Go 1.25、`net/http`+`go-chi/chi/v5`、`pgx/v5`、`shopspring/decimal`（**金錢禁止 float64**）、`golang-migrate`（純 SQL、embed 進執行檔、**啟動時自動 migrate up + advisory lock**，禁止 ORM AutoMigrate）、`log/slog` JSON、排程用 `robfig/cron/v3`。
- 前端：**Svelte 5.56 + SvelteKit 2 + Vite 8 + Tailwind v4 + TypeScript 6 + shadcn-svelte 1.3（bits-ui 2）**。圖表用 `layerchart@next`（v2，支援 Svelte 5），或退而用 `echarts` 6。**所有套件用最新版**。
- 部署：Docker Compose（`api`、`worker`、`postgres`、`ui` nginx 靜態）。

## 3. 目前 repo 狀態（重要）

**已 commit（main 分支，commit f84bc6f）**：M0–M2 後端骨架。
- 18 張表的 migration `0001_init`（users、api_keys、assets、ingestion_runs、market_daily_bars、corporate_actions、trading_calendar、ledger_events、lots、lot_consumptions、portfolio_snapshots、user_settings、strategy_versions、recommendation_runs/items、broker_snapshots/positions、reconciliation_runs/diffs、audit_log、idempotency_keys）。
- API server（chi、所有 v1 路由已掛、scope 檢查、idempotency middleware、session cookie + API key 雙認證）。
- `internal/`：auth（argon2id、API key 雜湊/輪替/撤銷）、ledger（FIFO 批次、費稅、void 重建）、twmarket（費稅算例已測）、strategy（再平衡純函式）、marketdata（TWSE 當日匯入）、recommend、reconcile。

**工作區未 commit 的進行中變更（前一個 agent 剛寫入，請當成基線、檢查正確性後續用，不要直接丟棄）**：
- 市場資料管線：`internal/marketdata/{backfill,calendar,corporate_actions,freshness,pipeline,ratelimit,rocdate,securities}.go` + `rocdate_test.go`，改了 `twse_importer.go`、`cmd/worker/main.go`、`config.go`，新增 `migrations/0002_market_pipeline.{up,down}.sql`。
- ledger 補完：`internal/ledger/{engine.go,engine_test.go}`，改了 `service.go`、`types.go`。
- 策略+回測：改 `internal/strategy/rebalance.go`+test，新增 `internal/backtest/`、`migrations/0003_backtest.{up,down}.sql`。
- 對帳補完：`internal/reconcile/{csv,engine,service_db_test}.go` + tests。
- 前端骨架：`web/`（SvelteKit + Svelte5 + Vite8 + Tailwind4 + TS6，**但 shadcn-svelte 尚未 init，`web/components.json` 不存在**）。

**你接手第一步**：`cd C:\Users\tingz\Documents\GitHub\easy-invest` →
```
go build ./...
go vet ./...
go test ./...
```
本機已有 docker postgres（`docker compose up -d postgres`，連線 `postgres://easy_invest:easy_invest@localhost:5432/easy_invest?sslmode=disable`）。需要 DB 的測試讀環境變數 `TEST_DATABASE_URL`，連不上就 skip。**先確認上面進行中的變更能編譯、測試綠燈，修掉任何破損，再往下做。**

## 4. 後端已知缺口（前一波若未完成就補完，並驗證）

對照 `docs/implementation-plan.md` 各里程碑驗收條件逐項確認：

1. **M3 市場資料管線**：catch-up（用交易日曆算缺口、逐標的逐月補、冪等斷點續傳）、`trading_calendar` 與 `corporate_actions` 實際匯入、每來源全域 rate limiter（TWSE 4 秒/請求+jitter、指數退避）、`/market/freshness` 要回每 dataset 最新資料日與落後交易日數、worker 改 cron（Asia/Taipei、每交易日 15:30 + 啟動 catch-up）。**TWSE 端點要實測並把結果回填 `docs/data-sources.md`**。`twse_importer.go` 原本有 bar_date 用「今天」而非資料交易日的 bug，確認已修。
2. **ledger 補完**：`split` 事件、`fee_adjustment`/`tax_adjustment`/`broker_reconciliation_adjustment`/`manual_correction` 四種調整事件、`lots.adjusted_cost` 雙視角成本維護（除息扣調整後成本、original_cost 不動）、portfolio 輸出 `avg_cost_original` 與 `avg_cost_adjusted`。`tw-market-rules.md` 五算例要有 ledger 層整合測試。
3. **M4 建議引擎**：`recommend` request body 改用嚴謹 decode、`strategy.Settings.CashBuffer` 必須真的生效（可動用現金 = 現金 − buffer，先賣後買、現金不足按比例縮單、縮到低於 min_trade_amount 剔除）、權重已在再平衡區間內輸出 hold/no_action、run 可從 inputs 完整回放（回放測試）、免責聲明與資料時間強制出現在回應。修 scope 不一致（PATCH recommendations 用了 read scope、GET broker-snapshots 用了 write scope）。
4. **M5 對帳**：六種 `diff_type`（quantity_mismatch、avg_cost_mismatch、missing_dividend、missing_position_internal/broker、fee_tax_mismatch）、`ResolveDiff` 的 `adjusted` 要在**同一 DB 交易**內建立可追蹤調整事件並回填 `adjustment_event_id`、CSV 解析器做成 per-broker plugin。
5. **M6 回測**：`internal/backtest` 純函式核心（重用 strategy.Rebalance，收盤價成交，算費稅）+ DCA/買進持有基準 + `backtest_runs` 表。**共用驗證**：回測最後一日輸出 = 線上引擎同日輸出。為回測加上 HTTP 端點（`POST /backtests/runs`、`GET /backtests/runs/{id}`）並補進 openapi.yaml。
6. **橫切**：把 `openapi.yaml` 補成完整合約（目前只有骨架，缺 17 條已實作路由與所有 request/response schema、錯誤格式、scope、Idempotency-Key、session 認證）。補 api/ledger/recommend 的測試。

## 5. 前端——這是重點，要求非常高

**目標**：每個畫面都精緻、實用、直覺、一看就會用。**不允許任何「留白沒做」的畫面**；每頁都要有完整的 loading（skeleton）、空狀態（含引導 CTA）、錯誤狀態（含重試）三態。可用現成元件庫（shadcn-svelte），但所有套件最新版。

### 5.1 先完成 shadcn-svelte 初始化

`web/` 已是 SvelteKit 骨架。CLI 互動式 init 卡在 preset 選單；請用 `npx shadcn-svelte@latest init`（base color: `neutral`，CSS 指向 `src/routes/layout.css` 或改用標準 `src/app.css`，alias 用 `$lib/components`、`$lib/components/ui`、`$lib/utils`、`$lib/hooks`），選預設 preset 完成設定，再 `add` 需要的元件（button、card、table、dialog、form、input、select、tabs、badge、sonner、skeleton、dropdown-menu、tooltip、sheet、separator、switch、sidebar 等）。

### 5.2 設計系統（請落實，不要敷衍）

- **配色遵循台股慣例：漲紅跌綠**（與美股相反，台灣使用者直覺）。漲跌色做成語意 token，不要散落 hardcode。
- 金額與數量一律 `tabular-nums` 等寬數字對齊。
- 字體：Noto Sans TC（自行打包，不依賴外部 CDN）。
- 版型：左側可收合 sidebar + 頂部常駐「資料新鮮度」chip（顯示 `market_data_as_of` 與落後天數，資料過舊變色警示）+ 深淺色主題切換。
- 用設計 token（CSS 變數）統一間距、圓角、陰影、字級；元件密度偏資訊型工具（緊湊但不擁擠）。

### 5.3 共用層鐵則（做成共用元件/工具，全站復用）

1. **金額/數量走字串 + decimal**：API 的金額數量是字串，前端用 decimal 庫（如 `decimal.js`）運算，**禁止轉 float 後再送回**。
2. **股/張雙顯示**：任何數量輸入必附 `unit`（share/lot，**必填，不可預設猜測**）；顯示同時呈現股與張（1 張=1000 股，依 `assets.lot_size`）。做成 `<QuantityInput>` 與 `<QuantityDisplay>`。
3. **免責聲明元件**：建議回應的 `disclaimer` 欄位 + 資料時間必須隨建議顯示。**全站禁止「保證」「穩賺」「必賺」等字眼**。
4. **事件不可改**：交易/事件 UI **不得有編輯、刪除**；只有 void（必填 reason）與新增調整事件。庫存是事件推導結果，不是可編欄位。
5. **錯誤信封**：統一解析 `{error:{code,message,details}}`，依 `code` 分流（含 `idempotency_replay`、`rate_limited`、`validation_failed`），表單錯誤用 `details[].field` 對應欄位。
6. **寫入 POST 帶 `Idempotency-Key`**（事件、建議 run、對帳）。
7. **API client**：型別化封裝 `/api/v1`，session cookie 認證 + CSRF，集中處理。

### 5.4 必做頁面（每頁完整規劃，不可留空）

1. **登入／註冊**：session 登入；註冊被環境變數關閉時要有提示；CSRF。
2. **儀表板/庫存**（`GET /portfolio`）：持股、平均成本（原始+調整後雙視角）、市值、未實現損益、現金、總覽卡片與權重圓環圖；**必顯示 `market_data_as_of`**；空狀態引導新增第一筆交易。
3. **批次明細**（`GET /portfolio/lots`）：同股多批次、原始成本 vs 調整後成本並列。
4. **交易/事件**（`/ledger/events`）：事件流（篩選 asset/type/from/to、cursor 分頁）；新增事件表單（**單位選擇器必填**、fee 留空=伺服器估算並標 `fee_source: estimated`、支援 buy/sell/配息/配股/減資/出入金/調整）；**只有 void，沒有編輯/刪除**。
5. **建議**（`/recommendations/*`）：觸發計算、歷史 run 清單、run 詳情；每個 item 顯示動作、標的、建議數量（股+張）、估價/估金額/估費稅、目前權重 vs 目標權重（視覺化）、**reason（後端人話直接顯示）**、risks、信心值；**必顯示 `market_data_as_of`、`portfolio_as_of`、`disclaimer`**；item 狀態操作 accepted/ignored（PATCH）。
6. **對帳精靈**（`/reconciliation/*`）：上傳券商 CSV/JSON、發起 run、逐筆檢視 diffs（六種 diff_type、internal vs broker 雙視角）、逐筆 resolve（adjusted/accepted_as_is/fixed_input），adjusted 連回產生的調整事件。
7. **回測**（若後端端點完成）：設參數、跑回測、權益曲線 vs 基準（DCA/買進持有）、績效指標（總報酬/年化/最大回撤）、交易清單。
8. **設定**（`GET/PUT /settings` + `GET /strategies`）：費率/折扣/低消、配息匯費、cash_buffer、min_trade_amount、prefer_whole_lot、risk_profile、目標權重編輯器（檢查總和）、rebalance_band；策略版本檢視。
9. **API key 管理**（`/api-keys`，僅 session）：建立（選 scopes、名稱、用途）、**明文只在建立當下顯示一次**、撤銷、輪替（含寬限期說明）、顯示 created_at/last_used_at/expires_at/revoked_at。
10. **資料新鮮度/市場資料**（`/market/freshness`、`/market/bars`、`/market/calendar`）：露出各來源最後匯入時間與健康狀態。

### 5.5 前端建置與部署

- `web/` 用 static adapter（已裝 `@sveltejs/adapter-static`）建成靜態檔，由 docker-compose 的 `ui`（nginx）服務，反代 `/api` 到 `api` 容器。補上 `web/Dockerfile`、nginx 設定，並把 `ui` 服務加進 `docker-compose.yml`。
- 開發時用 Vite proxy 把 `/api` 導到 `localhost:8080`。

## 6. 驗收與收尾

- 後端：`go build ./... && go vet ./... && go test ./...` 全綠；migration 從零跑到最新。
- 前端：`npm run build`、`npm run check`（svelte-check）無錯；逐頁實際開起來檢視三態與細節。
- 端到端：`docker compose up -d` 後，註冊→建 API key→新增多筆交易（含零股、配息、配股）→看庫存對得起來→產生建議（有理由+資料時間+免責）→對帳→設定，全流程跑通。
- 完成後做一次多角度 code review、同步 README 與 docs、commit & push（commit message 繁中，結尾加
  `Co-Authored-By:` 行）。

## 7. 慣例速記

- 繁中、無中國用語。
- 金錢 decimal；API 金額數量字串傳輸。
- ledger 事件不可 UPDATE 業務欄位，只能 void；修正走調整事件。
- 所有查詢掛 `user_id`；所有寫入記 `audit_log`。
- 策略/回測核心**禁止任何 I/O**（net/http、pgx、檔案）——資料由外層備妥再傳入。
- Insyra：做分析/指標/回測前先查 Insyra 能力，缺口開 upstream issue 並留 `TODO(insyra#n)`，同步 `docs/insyra-upstream-todo.md`，不要默默自建大型分析框架。