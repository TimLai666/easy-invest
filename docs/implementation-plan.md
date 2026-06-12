# 實作計畫

這份文件把概念規劃落成可動工的計畫：技術選型決策、程式碼結構、里程碑與驗收條件。目標不是程式練習，而是一套作者自己每天會用的系統，所以每個里程碑的出口條件都是「真實使用情境跑得通」，不是「程式碼寫完了」。

相關規格文件：

- [db-schema.md](db-schema.md)：資料庫 schema 草案與 migration 紀律。
- [api-spec.md](api-spec.md)：API 合約。
- [tw-market-rules.md](tw-market-rules.md)：台股費稅與公司行動計算規格（含驗收用算例）。

## 「真實可用」的定義

系統達到以下狀態才算第一版可用（對應 M4 完成）：

1. 每天收盤後，系統自動匯入台股日終資料，失敗會留下紀錄並可重跑。
2. 使用者新增一筆交易（含零股）在一分鐘內完成，費稅自動估算。
3. 庫存頁的股數、成本、未實現損益與券商 App 對得起來（差異可用對帳事件解釋）。
4. 按一個鍵取得當日建議，每筆建議都有人話理由、資料時間與風險聲明。
5. 全部資料存在自己的 PostgreSQL，`docker compose up -d` 可在任何機器重建，資料庫可備份還原。

## 技術選型決策

這些是定案，不再是「待討論」。要推翻必須在本文件留下紀錄與理由。

| 項目 | 決策 | 理由 |
|---|---|---|
| 語言 / 版本 | Go（go.mod 鎖最新穩定版） | 既定方向 |
| HTTP 框架 | `net/http` + `chi` router | 輕量、標準庫相容、middleware 生態夠用 |
| DB 存取 | `pgx/v5` + `sqlc` | SQL 可審查、型別安全；杜絕 AutoMigrate 與 ORM 魔法 |
| Migration | `golang-migrate`，純 SQL 檔，**啟動時自動執行**（advisory lock 防並發） | 可審查、可在 CI 驗證；部署即套用，規則見 db-schema.md |
| 金額運算 | `shopspring/decimal` | 金錢禁止 float64 |
| 密碼 / API key | argon2id / SHA-256 + 一次性顯示 | 業界慣例 |
| 資料處理與分析 | Insyra（DataTable/DataList/CCL） | 既定方向；缺口走 upstream issue（見 insyra-upstream-todo.md） |
| 排程 | 程式內 cron（如 `robfig/cron`）跑在 worker 容器 | 第一階段不需要外部 queue |
| UI | Vite + Svelte SPA，只呼叫 `/api/v1` | 輕、好寫儀表板；API-first 下 UI 可隨時整組換掉 |
| 部署 | Docker Compose：`api`、`worker`、`postgres`、`ui`（nginx 靜態） | 既定方向 |
| 設定 | 環境變數 + `.env`（compose 注入），不做設定中心 | 單機自用，從簡 |
| 日誌 | `log/slog` JSON 輸出 | 標準庫即可 |
| CI | GitHub Actions：`go vet`、`go test ./...`、migration 從零跑到最新、`golangci-lint` | 每個 PR 必過 |

懸而未決、各里程碑內解決的事項會在對應里程碑列出，不另開全域待辦。

## Repository 結構

```
easy-invest/
├── cmd/
│   ├── server/          # API server 進入點（啟動時自動 migrate）
│   ├── worker/          # 排程匯入、快照重算（啟動時自動 migrate）
│   └── migrate/         # 輔助命令：版本查詢、force、down、開發期重建
├── internal/
│   ├── api/             # HTTP handler、router、middleware、DTO
│   ├── auth/            # session、API key、scope 檢查
│   ├── ledger/          # 事件、批次、FIFO、費稅計算、快照（核心領域）
│   ├── marketdata/      # ingestion client（twse/tpex/mops）、正規化、入庫
│   ├── recommend/       # 組裝策略輸入、保存 run（有 I/O）
│   ├── strategy/        # 純計算策略核心（禁止任何 I/O，靠單元測試守住）
│   ├── backtest/        # 回測 harness，重用 strategy（M6）
│   ├── reconcile/       # 券商匯入解析、diff、調整事件產生
│   ├── twmarket/        # 台股規則：費稅、日曆、除權息公式（tw-market-rules.md 的程式化）
│   └── store/           # sqlc 產生碼 + repository
├── migrations/          # golang-migrate SQL
├── api/openapi.yaml     # API 機器可讀真源
├── web/                 # Svelte UI
├── deploy/docker-compose.yml
└── docs/
```

架構守則（CI 可用 import 檢查工具強制）：

- `strategy/` 與 `twmarket/` 不得 import `store/`、`net/http`、任何 driver。
- `api/` 不直接寫 SQL；一律經 `store/`。
- `ledger/` 是唯一能建立/void 事件與維護批次的套件。

## 里程碑

每個里程碑都以「可驗收情境」收尾。沒過驗收不開下一個。規模預估以一人業餘時間為基準，僅供排序參考。

### M0 — 骨架與地基（小）

內容：

- Go module、目錄結構、`chi` server、`/healthz`、`/version`。
- Docker Compose（api + postgres + worker 空殼）、`.env.example`。
- golang-migrate 接上：migration 以 embed.FS 打包，server 與 worker 啟動時自動跑到最新（advisory lock 防並發），失敗即終止。第一個 migration：`users`、`api_keys`。
- GitHub Actions CI：vet、test、lint、migration 從零跑到最新。

驗收：

- 乾淨機器 `docker compose up -d` 後 `curl localhost:8080/healthz` 回 200，含 DB 連線與 migration 版本——中間不需要任何手動 migration 步驟。
- api 與 worker 同時冷啟動（資料庫全空）不互撞，migration 只被執行一次。
- CI 綠燈。

### M1 — 身分、API key、資產主檔（小）

內容：

- 註冊（可用環境變數關閉）、登入、session、`/me`。
- API key 建立/撤銷/輪替/scope 檢查、audit log。
- `assets` 表 + TWSE/TPEx 上市櫃清單匯入命令（含 ETF 標記），`GET /assets` 搜尋。

驗收：

- 用 curl 拿 API key 操作 `/assets?query=台積`，撤銷後同一把 key 立刻 401。
- 資產清單包含上市、上櫃與 ETF，0050 的 `asset_type` 是 `tw_etf`。

### M2 — Ledger 核心（大；本專案的心臟）

內容：

- `ledger_events` 全事件類型寫入與 void、`lots`、FIFO 沖銷、`lot_consumptions`。
- `twmarket` 套件：手續費、證交稅、健保補充保費、捨入規則，**tw-market-rules.md 的五個算例全部寫成單元測試**。
- 現金部位追蹤（`cash_deposit`/`cash_withdraw` + 各事件 `cash_delta`）。
- `portfolio_snapshots` 維護與 `GET /portfolio`、`GET /portfolio/lots`。
- 原始成本與調整後成本兩種視角。
- UI：登入、新增交易表單（股/張切換）、庫存頁、事件流水頁。

驗收（第一個真實可用點——可當記帳工具日用）：

- 把自己真實的歷史交易（至少含：整張買進、零股買進、部分賣出、一次現金股利、一次配股）全部輸入後，`GET /portfolio` 的持股股數與平均成本和券商 App 一致或差異可解釋。
- void 一筆中間的買進事件後，批次與快照重算正確（測試覆蓋）。
- 事件表不存在任何 UPDATE 業務欄位的路徑（code review + repository 層測試）。

### M3 — 市場資料管線（中）

內容：

- worker 匯入 TWSE/TPEx 日終行情、公司行動（除權息）、交易日曆。**排程語意是 catch-up 不是 daily**：每次啟動與每個週期都以交易日曆計算缺口、逐日逐標的補齊（停機多久都能追上），規則見 data-sources.md「補抓與資料連續性」。來源端點以 docs/data-sources.md 優先序為準，**動工第一件事是驗證實際 API 路徑與格式（含個股歷史端點能否回補），並把結果回填 data-sources.md**。
- 追蹤範圍：持有過的標的 + 關注清單 + 基準標的；新標的初次買進時自動回補歷史窗口。
- 抓取防鎖：每來源全域 rate limiter（預設 3–5 秒/請求 + jitter）、backfill 序列執行、指數退避、標的×月份斷點續傳、原始回應落地，規則見 data-sources.md「抓取速率與封鎖防護」。
- `ingestion_runs` 紀錄、失敗重試、手動重跑命令、來源修正時 revision 處理、無法回補的缺口標記 `unrecoverable`。
- 建議引擎資料新鮮度防線：資料落後標示天數，落後超過門檻（預設 5 個交易日）拒絕產生建議。
- `GET /market/*` 端點與 `market/freshness`（含每來源健康狀態與實際供資來源）；備援切換、主來源恢復後覆核、抽樣比對與 `disputed` 標記，規則見 data-sources.md「來源矩陣」「來源健康狀態」「來源衝突處理」。
- 庫存頁顯示市值與未實現損益（標示資料日期）；`portfolio/history` 每日淨值序列。
- 績效計算：已實現/未實現損益、TWR 或 XIRR——先查 Insyra 能力，缺口開 upstream issue（見 insyra-upstream-todo.md）。

驗收：

- 連續運轉一週，每個交易日資料自動入庫；某日來源故障時 `ingestion_runs` 有失敗紀錄且手動重跑成功。
- **停機測試**：故意關機跨過至少三個交易日後重啟，worker 自動補齊缺口，`portfolio/history` 淨值序列無空洞；補不回來的資料集在 freshness 中如實標示。
- 假日不誤跑（交易日曆生效）。
- 庫存市值與券商 App 收盤後顯示一致。

### M4 — 建議引擎 v1（中）

內容：

- `strategy_versions` + 策略核心 v1：**目標權重再平衡**。輸入：持股權重、目標權重、再平衡區間、現金緩衝、最小交易金額、整張偏好、費稅估算。輸出：每標的 buy/sell/hold + 數量 + 理由文字。
- `recommend/` 組裝輸入（庫存快照 + 最新收盤 + 設定），保存 `recommendation_runs/items`，inputs 完整可回放。
- 免責聲明與資料時間註記強制出現在 API 回應與 UI。
- worker 每日收盤資料入庫後自動產生一次建議（可關）。
- UI：建議頁（理由、權重圖、採納/忽略）、設定頁（目標權重、費率、風險參數）。

驗收：

- 用真實庫存設一組目標權重，產生的建議數量*價格加回現有部位後，各標的權重落在目標 ±再平衡區間內；費稅估算與 twmarket 公式一致。
- 權重已在區間內時，輸出為 hold/no_action 而不是硬湊交易。
- 任一筆歷史 run 可從 `inputs` 重算出相同 items（回放測試）。
- 策略核心套件 import 檢查：無任何 I/O 依賴。

### M5 — 券商對帳（中）

內容：

- 券商 CSV 匯入（先支援自己實際使用的券商格式，解析器設計成 per-broker plugin）。
- `broker_snapshots/positions`、diff 引擎（股數、平均成本、漏記配息、多缺部位）、`reconciliation_runs/diffs`。
- resolve 流程：UI 逐筆確認 → 產生 adjustment 事件（同交易），或標記「接受差異」。
- UI：對帳精靈。

驗收：

- 匯入真實券商庫存檔，系統列出的差異全部可歸因；逐筆 resolve 後再跑一次對帳，差異為零或全部是「已接受」狀態。
- 對帳產生的調整事件可追蹤到 diff 與 snapshot（外鍵串得起來）。

### M6 — 回測與策略迭代（大，可分批）

內容：

- `backtest/` harness：重用 strategy 核心，輸入歷史行情切片，模擬費稅與成交（以收盤價成交的保守假設），輸出績效與基準比較。
- 基準：定期定額（參考文件的 Ghost DCA 概念，改台股語境）與買進持有。
- walk-forward 驗證、參數掃描；GA 最佳化列為選配，先確認 Insyra 能力與 upstream issue。
- 報表：回測結果存檔可重現（種子、參數、資料版本）。

驗收：

- 同一策略版本，回測模擬最後一日的建議輸出 = 線上引擎用同日資料的輸出（核心共用驗證）。
- 對 0050 定期定額基準的回測結果可手算抽查兩個月份無誤。

### 後續（不排程）：券商唯讀 API connector、近即時行情、虛擬貨幣 connector、多使用者對外開放（需要再過一輪安全與合規檢視）。

## 橫切關注點

### 測試策略

- `twmarket/`、`ledger/`、`strategy/`：表格驅動單元測試，覆蓋 tw-market-rules.md 全部算例與邊界（零股低消、健保門檻 19999/20000、void 重算、FIFO 跨批次）。
- `store/`：以 testcontainers（或 compose 起的測試 DB）跑 repository 整合測試。
- API：每個里程碑的驗收情境寫成 e2e 測試（Go httptest 或腳本），CI 全跑。
- 回歸保護：建議 run 回放測試（同 inputs 必同 outputs）。

### 安全

- 見 api-spec.md 安全要求。另外：DB 不對外開 port（compose 內網）、備份檔加密、`.env` 不進 git。
- 即使單人自用，所有資料存取都掛 user_id——多使用者是架構假設，不是未來重構。

### 備份

- worker 每日 `pg_dump` 到本機卷 + 保留 14 份輪替；還原步驟寫進 README 並實際演練一次（M3 驗收附帶項目）。

### 合規呈現

- UI 與 API 的建議輸出固定附 tw-market-rules.md 的免責聲明與資料時間。
- UI 不得出現「保證」「穩賺」等字眼；績效一律標示計算區間與方法。

## 風險清單

| 風險 | 影響 | 對策 |
|---|---|---|
| TWSE/TPEx OpenAPI 格式變動或限流 | 匯入中斷 | ingestion 失敗可重跑、來源抽象成 interface、freshness 顯示讓使用者知道資料舊了 |
| 券商成本算法與系統不一致 | 使用者不信任數字 | 雙視角成本 + 對帳事件吸收差異，是設計核心而非補丁 |
| Insyra 缺金融計算能力 | M3/M6 延誤 | 先查能力→開 upstream issue→最小暫時實作標 TODO(insyra#n)，不自建框架 |
| 公司行動資料品質（MOPS 格式難解析） | 配息配股算錯 | 公司行動允許手動補登；自動匯入資料標示來源供查核 |
| 一人專案範圍蔓延 | 永遠不可用 | 里程碑出口條件硬性把關；M2 完成即有日用價值 |
