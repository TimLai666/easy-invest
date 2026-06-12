**文件目標:** 定義 QuantSaaS 全域有哪些物理端、有哪些邏輯模塊、狀態如何在它們之間流轉，以及系統整體的生命週期動作。不含任何具體策略公式。

---

## 1. 架構哲學：時空正交與無記憶解算

**動作原則：** 所有業務推進必須被封裝為離散的"動作"，動作之間嚴禁隱式狀態傳遞（禁止"欠債記錄"）。每個動作讀入當前狀態快照，產出新的確定性意圖，不依賴上一動作的記憶體變數。

**窗口原則：** 每個模塊只能讀取屬於自己的"資料窗口"和"資金窗口"。策略模塊看不到資料庫內部狀態，宏觀引擎看不到微觀的浮倉細節，進化引擎看不到實際交易帳戶。


---

## 2. 三端物理部署形態

### 2.1 SaaS 管理端（雲端，`app_role: saas`）

對策略模板、執行個體、使用者認證與訂閱進行全量管理。透過 cron 掃描 RUNNING 執行個體，並僅在該執行個體最新已完成的 `[聚合週期]` 聚合桶尚未處理時執行一次 `Step()`。透過 WebSocket Hub 下達交易指令給連線中的 LocalAgent，並接收成交上報。

**不執行：** GA 進化與回測運算（可透過 `app_role` 在路由層攔截寫介面）。

**不保存：** 任何券商 API 憑證。


### 2.2 LocalAgent 執行端（使用者本地）

極簡二進位進程，使用者本地運行。配置檔案（`config.agent.yaml`）內保存 SaaS 連線資訊與 [你的券商] API 憑證。

職責：
1. 啟動時透過 SaaS REST 登入取得 JWT
2. 用 JWT 建立 WebSocket 長連線到 SaaS `/ws/agent`
3. 收到 `TradeCommand` 後呼叫 [你的券商] REST API 執行 A 股/黃金 ETF 下單
4. 取得成交明細與當前餘額，建置 `DeltaReport` 上報 SaaS
5. 定時送出心跳；斷線後以指數退避自動重連

**不含：** 任何策略程式碼，不連線資料庫，不自主決策。


### 2.3 進化算力面（本地實驗室，`app_role: lab`）

也是完整 SaaS 系統，但部署在本地算力機器上。資料庫連線雲端 SaaS 的同一 Postgres 執行個體，專注執行 GA 進化任務與回測運算。

**不執行：** 真實交易指令下達（`app_role: lab` 在路由層停用執行個體啟停與交易下達）。


### 2.4 `app_role` 三態行為矩陣

| 角色 | 部署場景 | 開放能力 | 限制 |
|------|---------|---------|------|
| `saas` | 雲端 SaaS | 執行個體管理、交易指令下達、使用者營運 | 禁止進化/回測寫介面 |
| `lab` | 本地實驗室 | GA 進化、回測、基因庫管理 | 禁止交易指令下達、執行個體唯讀 |
| `dev` | 開發測試 | 所有模塊全開 | 無限制 |


---

## 3. 邏輯模塊與責任邊界

### 3.1 Strategy 策略模塊（產品中心）

管理策略模板（圖紙）。定義策略的參數結構、版本標識、標的類型（A 股 ETF/個股、黃金 ETF/現貨等）。當前主線策略為 `[your-strategy-id]`，面向 [你的券商] [你的標的程式碼]（如 510300.SH、518880.SH）。

`Step()` 是策略決策的唯一外部入口，所有買賣意圖、部位規模、風控判斷均在 `Step()` 內產生。回測路徑與實際交易路徑共用同一套 `Step()` 實現，不允許內部分叉。


### 3.2 Live/Instance 交易執行模塊（設備中心）

執行個體（Instance）的本質是：策略模板 + 交易標的 + 資金配額 + 使用者授權。執行個體的建立、啟動、停止、刪除，以及運行中的狀態（`RUNNING` / `STOPPED` / `ERROR`）全部由此模塊管理。

cron 按基礎掃描頻率檢查所有 `RUNNING` 狀態執行個體；執行個體 tick 只有在最新已完成的 `[聚合週期]` 聚合桶未處理過時才推進：拉取公開 K 線資料，讀取 `PortfolioState`，建置 `StrategyInput`，呼叫 `Step()`，將產出的交易意圖翻譯為 `TradeCommand`，透過 WebSocket Hub 下達給已連線的對應 Agent。


### 3.3 Evolution & Backtest 模塊（參數實驗室）

透過 GA 遺傳演算法對策略參數進行進化迭代，產出 **挑戰者（challenger）** 參數包。人工在前端審批後可將挑戰者晉升為 **冠軍（champion）**。冠軍參數包用於執行個體建立或參數更新。

只在 `app_role: lab` 或 `dev` 下開放進化任務建立與執行，回測路徑呼叫與實際交易相同的策略 `Step()` 實現。


### 3.4 User/Auth 模塊

處理使用者注冊、登入、JWT 簽發，以及訂閱計劃校驗（建立執行個體、啟動執行個體時做配額守門）。此模塊不涉及策略邏輯，不在本文件細化。


---

## 4. 全域狀態總線

### 4.1 單一 Postgres + 單一 Redis

整個系統使用單一 Postgres 資料庫（`quantsaas`），不再區分 Shell DB 與 Live DB。Redis 僅作快取使用（冠軍基因 key、會話等），不承擔信號傳遞職責。


### 4.2 資料唯一來源與所有權

| 資料 | 唯一來源 | 說明 |
|------|------|------|
| 策略決策邏輯 | `Step()` 實現（SaaS 側執行） | 回測與實際交易必須共用 |
| 冠軍參數包 / 模板 | SaaS DB | 進化產出，供執行個體綁定 |
| 終端使用者與訂閱 | SaaS DB | 統一在 SaaS 管理 |
| 運行中執行個體狀態 | SaaS DB | 由 Agent 上報後維護 |
| 券商 API 憑證 | LocalAgent 本地配置檔案 | 僅 Agent 持有，永不進入 SaaS DB |
| 成交記錄與資產快照 | SaaS DB | Agent 上報後持久化 |
| GA 任務與基因記錄 | SaaS DB | 單一資料庫，無分庫 |


### 4.3 券商憑證物理隔離硬性規則

[你的券商] API Key / Secret（及券商若要求的附加鑒權字段）**只能**存在於 LocalAgent 的本地配置檔案（`config.agent.yaml`）中。

- 永不進入 SaaS 服務
- 永不寫入任何資料庫表
- 永不透過網路傳輸到雲端

發現程式碼將 API Key 寫入 SaaS 側時，必須立即停止並報告。


---

## 5. IoT 下達與上報通信協議

雲端與端側嚴格遵循"**雲端只信上報，端側無腦執行**"機制，徹底解決網路斷線、丟包與臟資料問題。

### 5.1 WebSocket 長連線模型

Agent 發起連線，SaaS 監聽 `/ws/agent` 端點。連線建立時 Agent 必須先送出含 JWT 的鑒權消息，SaaS 驗證透過後返回鑒權結果，之後進入正常收發循環。Agent 每 30 秒送出一次心跳，SaaS 回確認。


### 5.2 消息類型全表

| 消息類型 | 方向 | 觸發時機 |
|---------|------|---------|
| `auth` | Agent → SaaS | 連線建立後立即送出，攜帶 JWT |
| `auth_result` | SaaS → Agent | 鑒權完成後返回 |
| `heartbeat` | Agent → SaaS | 每 30 秒 |
| `heartbeat_ack` | SaaS → Agent | 收到心跳後 |
| `command` | SaaS → Agent | `Step()` 產出意圖後，SaaS 翻譯為具體指令下達 |
| `command_ack` | Agent → SaaS | 收到 command 後立即確認（不等執行完成） |
| `delta_report` | Agent → SaaS | 交易執行完成後；或重連時的初始餘額快照 |
| `report_ack` | SaaS → Agent | SaaS 成功處理 DeltaReport 後 |


### 5.3 TradeCommand 語義

每條 `TradeCommand` 攜帶以下語義字段：

| 字段 | 取值范圍 | 說明 |
|------|---------|------|
| `client_order_id` | 全域唯一字符串 | 格式：`inst{id}-{type}-{ts}`，用於去重與審計 |
| `action` | `BUY` / `SELL`（大寫） | 交易方向 |
| `engine` | `MACRO` / `MICRO`（大寫） | 產生此指令的引擎層 |
| `symbol` | 如 `510300.SH`、`518880.SH` | 標的程式碼 |
| `amount_cny` | 字符串金額 | 買入時使用，表示花費人民幣（CNY）數量 |
| `qty_asset` | 字符串數量 | 賣出時使用，表示賣出標的資產數量 |
| `lot_type` | `DEAD_STACK` / `FLOATING` | 部位語義，宏觀吸入底倉 vs 微觀浮動倉 |

SaaS 下達 TradeCommand 的同時在資料庫寫一條 pending 記錄，供 DeltaReport 回填時按 `LotType` 拆分更新 `DeadStack` / `FloatStack`。


### 5.4 DeltaReport 語義

Agent 執行完畢後上報，包含：
- `balances`：當前所有資產的餘額快照（標的份額與 CNY 現金的可用與凍結）
- `execution`：本次成交明細（成交量、成交價格、手續費、狀態）
- `client_order_id`：對應的指令 ID（重連時送出初始快照時可為空）


### 5.5 狀態收斂與天然自愈

SaaS 收到 DeltaReport 後刷新對應執行個體的 `PortfolioState`，持久化 `SpotExecution` 與 `TradeRecord`，並寫審計紀錄。

下次 `Step()` 呼叫時，系統基於真實餘額與語義拆分重新決策。這意味著：
- Agent 斷線後 SaaS 跳過該執行個體的 tick，無須人工干預
- Agent 重連後立即送出初始餘額快照，SaaS 在下次 tick 自動糾偏
- SaaS 重啟後從 DB 恢復 RUNNING 執行個體列表，等待 Agent 重連


---

## 6. 系統級生命週期動作

### 6.1 系統初始化（System Init）

**觸發時機：** SaaS 進程啟動時。

**動作流轉：**
1. 讀取配置檔案（`config.yaml`），初始化 DB 連線、Redis 連線、JWT 秘鑰
2. 執行 GORM AutoMigrate，以 Go struct 為唯一 schema 唯一來源完成資料庫結構同步
3. 從 DB 讀取所有狀態為 `RUNNING` 的執行個體，注冊到記憶體管理列表
4. 啟動 WebSocket Hub，開始接受 Agent 連線
5. 啟動 cron 調度器，注冊執行個體基礎掃描任務；具體 `Step()` 推進由每個執行個體的聚合桶去重控制


### 6.2 Cron Tick 驅動執行個體 Step()

**觸發時機：** cron 基礎掃描命中 `RUNNING` 執行個體後，若該執行個體最新已完成的聚合桶尚未處理，則觸發一次 `Step()`；已處理過同一桶時跳過，避免實際交易比回測多跑決策。

**動作流轉：**
1. 從行情資料源（交易所公開行情或第三方行情 API）拉取 K 線歷史資料
2. 從 DB 讀取執行個體的 `PortfolioState`（由上次 DeltaReport 維護的真實帳戶狀態）
3. 從 DB 讀取 `RuntimeState`（策略內部運行狀態快照）
4. 建置 `StrategyInput`（含 K 線序列、資產狀態、時間戳）
5. 呼叫 `[策略名].Step()`（**只在 SaaS 側執行**，Agent 不含此邏輯）
6. 將 `Step()` 產出的交易意圖翻譯為 `TradeCommand`（含全域唯一 `client_order_id`）
7. 在 DB 寫入 pending 執行記錄
8. 透過 WebSocket Hub 將 `TradeCommand` 下達給對應 Agent

若 Agent 當前未連線，則跳過下達，記錄警告，等待下次 tick 時重試。


### 6.3 Agent 斷線重連

**觸發時機：** Agent 檢測到 WebSocket 連線斷開。

**動作流轉：**
1. 等待初始退避時間（1 秒），嘗試重新登入取得 JWT
2. 建立新的 WebSocket 連線
3. 連線建立後立即送出初始 `DeltaReport`（當前券商帳戶餘額快照，不含 `client_order_id` 與 `execution`）
4. SaaS 收到快照後更新相關執行個體的 `PortfolioState`
5. 下次 cron tick 時，SaaS 基於真實餘額重新決策，無需人工干預

重連退避策略：初始 1 秒，每次翻倍，最大等待 5 分鐘。


### 6.4 優雅停機與狀態快照（Graceful Shutdown）

**觸發時機：** SaaS 進程接收到操作系統中斷信號（如 `SIGTERM`）。

**動作流轉：**
1. 停止接受新的 Agent 連線與 cron tick 任務
2. 等待正在執行中的 tick 完成（超時保護）
3. 持久化所有活躍執行個體的 `RuntimeState` 快照，避免重啟後策略狀態撕裂
4. 關閉所有 WebSocket 連線，送出關閉幀
5. 關閉 DB 與 Redis 連線


---

## 7. 不可推翻的技術決策

以下為整個系統的硬約束，違反時必須停止開發並報告，不得繞過。

**1. 復利前置條件（硬性規則）**
任何涉及策略機制的設計或實現，必須能清楚說明復利如何發生（資金規模隨權益正反饋滾動）。不滿足可論證復利性質的策略設計，不進行開發。


**2. 策略同構原則**
回測與實際交易必須呼叫同一個 `Step()` 實現。`Step()` 內部禁止出現 `if isBacktest { ... } else { ... }` 類分支。`Step()` 只在 SaaS 側執行，Agent 二進位不包含任何策略程式碼。


**3. 策略內部純凈原則**
策略程式碼內部絕對禁止：定時器、網路請求（HTTP / WebSocket / gRPC）、資料庫讀寫、任何檔案 I/O。策略是純函數，只依賴傳入的 `StrategyInput`，只輸出 `StrategyOutput`。


**4. 現貨類標的策略 ACL OHLC 剝離**
`Manifest.IsSpot == true` 表示 A 股/黃金等現貨類、非杠桿衍生品標的。此類策略內核程式碼禁止依賴 `OHLCV` 聚合結構（`quant.Bar`）。所有 OHLCV 到 `[]float64 + []int64`（收盤價序列 + 時間戳序列）的降級必須在 ACL 外圈（回測適配器入口與實際交易 tick 入口）完成。


**5. API Key 物理隔離**
[你的券商] API Key / Secret 只允許存在於 LocalAgent 的本地配置檔案中，永不進入 SaaS，永不寫入資料庫，永不透過網路傳輸。


**6. GORM AutoMigrate 為唯一 schema 唯一來源**
資料庫結構以 Go struct 為唯一來源，透過 GORM AutoMigrate 管理。不寫 SQL migration 檔案，不維護版本化 migration 腳本。


**7. 無量綱計算**
所有價格相關計算使用對數收益率或比率（無量綱），禁止用絕對價格做跨標的比較。


**8. 部位三態**
資產狀態以 `DeadStack`（宏觀長期底倉）/ `FloatStack`（微觀浮動倉）/ `ColdSealedStack`（冷封存，永不釋放）三態區分語義，不能混用。部位三態的更新規則由策略 `Step()` 決定，SaaS 按 `LotType` 字段落賬。


**9. 底倉釋放僅在 SaaS 側更新，不下達 Agent**
`dead_stack → floating` 的語義轉換只改變 SaaS 側的 `PortfolioState` 賬本，不向 Agent 下達任何 `TradeCommand`，但必須寫審計紀錄。


**10. 單一資料庫，無分庫**
SaaS 使用單一 Postgres 執行個體，不分 Shell DB 與 Live DB，不使用 Redis 作為信號傳遞通道（Redis 僅作快取）。


**11. 使用者界面零數學**
面向使用者的字符串中避免出現內部狀態機術語、無上下文的裸數學量名或公式片段。希臘字母並非一律禁止；若含義上必須保留，應采用「字母 + 文字釋義」並置（例如「σ（標準差）」「ρ（相關系數）」），禁止僅展示希臘字母而無對應說明。
