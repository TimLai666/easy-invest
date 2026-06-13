# 資料來源

## 原則

Easy Invest 的建議品質取決於資料品質。所有市場資料、公司行動、券商帳務與成交紀錄都必須能追蹤來源、抓取時間、資料時間與匯入狀態。

策略核心不得直接抓外部資料。資料必須先由 ingestion 流程匯入 PostgreSQL，經過檢查與標準化後，才能進入建議引擎。

## 台股資料優先序

第一優先是官方或授權資料：

- TWSE OpenAPI：`https://openapi.twse.com.tw/`
- TWSE 個股日成交資訊：`https://www.twse.com.tw/zh/trading/historical/stock-day.html`
- TPEx OpenAPI：`https://www.tpex.org.tw/openapi/`
- 公開資訊觀測站：`https://mops.twse.com.tw/`
- 明確授權的資料商。
- 券商 API 回傳的使用者帳務、庫存與成交資料。

非官方來源只能做輔助比對，不可當唯一真源。若某個資料只能透過非官方來源取得，必須在文件中標明風險與替代方案。

## 來源矩陣（每個 dataset 的主來源與備援）

全部規劃來源皆為免費公開資料，無 API key、無授權費。下表端點已於 2026-06-13 以 `go run ./cmd/verify-marketdata` 實測可連線、可解析 JSON，並以該指令作為端點層驗證證據。資料庫層驗證使用 `cmd/marketdata-live-check`：套用 migration、同步上市櫃資產清單、執行 catch-up、抽查 TWSE 0050 與 TPEx 006201 歷史月回補，最後從 PostgreSQL 讀出資料表筆數、樣本行情與 freshness。兩個驗證命令預設每次官方 API 請求至少間隔 4 秒；正式 worker 也使用每來源全域 rate limiter，避免短時間連打。

| Dataset | 主來源 | 備援 / 輔助比對 | 備註 |
|---|---|---|---|
| 上市日終行情（當日全市場） | `https://openapi.twse.com.tw/v1/exchangeReport/STOCK_DAY_ALL` | FinMind（非官方，僅輔助） | JSON 陣列；欄位含 `Date`、`Code`、`Name`、`OpeningPrice`、`HighestPrice`、`LowestPrice`、`ClosingPrice`、`TradeVolume`、`TradeValue`。僅當日快照，不可回補。 |
| 上市日終行情（歷史回補） | `https://www.twse.com.tw/exchangeReport/STOCK_DAY?response=json&date=YYYYMM01&stockNo=代號` | FinMind | `stat=OK` 時 `data` 為 `[日期, 成交股數, 成交金額, 開盤價, 最高價, 最低價, 收盤價, 漲跌價差, 成交筆數, 註記]`。限流敏感，走防鎖規則。 |
| 上櫃日終行情（當日） | `https://www.tpex.org.tw/openapi/v1/tpex_mainboard_daily_close_quotes` | FinMind | JSON 陣列；欄位含 `Date`、`SecuritiesCompanyCode`、`CompanyName`、`Open`、`High`、`Low`、`Close`、`TradingShares`、`TransactionAmount`。僅當日快照。 |
| 上櫃日終行情（歷史回補） | `https://www.tpex.org.tw/www/zh-tw/afterTrading/tradingStock?date=YYYY/MM/01&code=代號&response=json` | FinMind | `stat=ok` 時 `tables[0].data` 為 `[日期, 成交仟股, 成交仟元, 開盤, 最高, 最低, 收盤, 漲跌, 筆數]`；匯入時轉成股數與新台幣元。 |
| 除權息／公司行動 | `https://openapi.twse.com.tw/v1/exchangeReport/TWT48U_ALL` + TPEx 對應表 | MOPS 公告、FinMind | TWSE 欄位含 `Date`、`Code`、`Name`、`Exdividend`、`StockDividendRatio`、`CashDividend` 等。可事後回補；MOPS 解析成本高，列輔助。 |
| 減資、現增等公司行動細節 | MOPS 公告 | 手動補登 | 頻率低，允許人工 |
| 交易日曆 | `https://www.twse.com.tw/rwd/zh/holidaySchedule/holidaySchedule?date=YYYY0101&response=json` | 手動維護年度假日表 | `stat=ok` 時 `data` 為 `[日期, 名稱, 說明]`。每年抓一次整年；TWSE 與 TPEx 第一階段共用同一份台股開休市日曆。 |
| 上市櫃資產清單 | TWSE/TPEx OpenAPI 證券清單 | 手動補登 | 每週同步一次即可 |
| 券商帳務 | 使用者匯出 CSV | — | 不是抓的，使用者自己給 |

API key 需求：官方來源（TWSE/TPEx OpenAPI、查詢端點、MOPS）全部不需要 key 也不需註冊。FinMind 匿名可用但額度很低，註冊免費帳號取得 token 後額度較高（實際額度 M3 實測確認）；token 放環境變數，視同機密管理，但屬「可選設定」——沒設 token 系統照常運作，只是 FinMind 備援額度受限。

FinMind 為社群維護的免費資料 API（有請求額度），屬非官方來源：只能用於「主來源壞掉時的臨時資料」與「抽樣比對」，由它來的每筆資料都標記來源，**不得與官方資料混在同一序列裡不可區分**。

## 來源健康狀態與備援切換

「誰壞了」必須是系統裡看得到的事實，不是去翻 log 猜：

- 每個（來源 × dataset）維護健康狀態：`healthy` / `degraded`（連續失敗中、退避中）/ `paused`（連續失敗達上限）。狀態由 `ingestion_runs` 的成功失敗紀錄推導，`GET /market/freshness` 回傳每個 dataset 的：最新資料日、主來源狀態、實際供資來源。
- 切換規則：主來源 `paused` 時才啟用備援，且備援抓回的資料一樣寫 `ingestion_runs` 與來源標記；主來源恢復後，**用主來源資料重新覆核備援期間的資料**（產生新 revision），備援資料不會默默變成永久真源。
- UI 儀表板顯示資料來源狀態列：哪個 dataset 目前由誰供資、是否落後、是否有未覆核的備援資料。

## 來源衝突處理（同一筆資料兩個來源不一致）

- 每筆入庫資料都帶來源標記，**不同來源的資料不混算、不平均**。
- 正常情況只用主來源，不會發生衝突；衝突只出現在兩種場景：備援期資料被主來源覆核時，以及例行抽樣比對時（每週隨機抽 N 個標的×日期，主來源 vs 輔助來源）。
- 比對規則：收盤價、股數完全相等才算一致（日終資料沒有「差不多」的空間）；不一致就建立 discrepancy 紀錄（dataset、標的、日期、兩邊數值、兩邊來源）。
- 處理順序：官方來源優先於非官方；同為官方（如 TWSE vs MOPS）出現不一致時不自動裁決，標記該筆資料 `disputed`，等人工確認。
- `disputed` 資料對下游的影響：建議引擎照常運作，但引用到 disputed 資料的建議必須在風險欄位註明；對帳報告引用到時同樣標示。
- 衝突解決後寫入新 revision，舊值保留可追溯（沿用 `market_daily_bars.revision` 機制）。

## 資料新鮮度分級

### 即時或近即時資料

用途：

- 儀表板顯示最新價格。
- 估算目前市值。
- 對短線訊號做輔助判斷。

限制：

- 必須標示行情時間與延遲。
- 不可把延遲行情包裝成即時行情。
- 若資料商授權或穩定性不足，不可用於自動下單。

### 日終資料

用途：

- 長期投資建議。
- 回測。
- 策略參數最佳化。
- 每日庫存市值與績效計算。

日終資料穩定性通常比即時資料高，第一階段建議以日終資料作為主要策略依據。

### 公司行動資料

用途：

- 現金股利。
- 股票股利。
- 除權息。
- 分割。
- 減資。

公司行動會影響成本、股數與對帳結果，必須獨立建模，不可只靠價格序列推回去。

### 券商帳務資料

用途：

- 對帳。
- 查詢實際庫存。
- 查詢成交紀錄。
- 查詢券商顯示成本。

券商帳務代表外部事實，但不直接覆蓋內部 ledger。系統應保存券商 snapshot，再由對帳流程產生差異與調整事件。

## 匯入紀錄

每批匯入至少記錄：

- `source_name`
- `source_url`
- `source_license`
- `fetched_at`
- `data_time`
- `market`
- `asset_type`
- `symbol`
- `status`
- `checksum`
- `error_message`

若來源資料後續修正，必須能重跑匯入並保留版本差異。

## 補抓與資料連續性（backfill）

系統可能停機數天（停電、搬家、單純沒開機）。匯入管線不可設計成「每天跑一次今天的資料」，必須設計成「把資料補齊到最新」，停多久都能自己追上。

原則：

- **排程語意是 catch-up，不是 daily**。worker 每次啟動與每個排程週期都執行同一件事：對每個 dataset 計算「應有的交易日集合 −已入庫的資料日集合」，得到缺口清單，逐日補抓。冪等：同一天重抓只會比對與覆寫同版資料，不會重複。
- **缺口由交易日曆判定**。先補日曆，再補其他 dataset；日曆本身每年公告一次，提前抓整年，停機期間不影響判定。
- **追蹤範圍要收斂**。日終行情不需要全市場歷史：補抓範圍 = 使用者持有過的標的 + 關注清單 + 基準標的（如 0050、加權指數）。全市場端點通常只提供當日快照，停機漏掉就拿不回來；歷史補抓必須走「個股歷史」端點（如 TWSE 個股日成交資訊可按月查），逐標的逐月補。這也是把追蹤範圍收斂的原因——per-symbol 補抓才補得完。
- **公司行動與歷史除權息**可事後查詢，停機後照缺口補；補入後觸發受影響期間的還原價與淨值序列重算。
- **新標的初次入庫**走同一套機制：使用者第一次買進某標的時，從最早持有日（再往前推策略所需的歷史窗口）補抓到最新，不是只抓當天。
- **補不到的資料要誠實**。若來源確實無法回補（例如僅當日提供的資料集），缺口標記為 `unrecoverable`，`market/freshness` 與 UI 明確顯示，不得以內插或前值填補冒充真實資料。

對建議引擎的保護：

- 產生建議前檢查資料新鮮度：最新資料日 < 最近一個交易日時，建議仍可產生，但必須標示「資料落後 N 個交易日」；落後超過使用者設定門檻（預設 5 個交易日）則拒絕產生，提示先補資料。

## 抓取速率與封鎖防護

TWSE/TPEx 對高頻請求會擋（429、空回應、甚至封 IP），逐標的逐月的 backfill 是最容易觸發的場景。以下是匯入管線的硬性規則，不是最佳化選項：

- **每個來源一個全域 rate limiter**：同一來源的所有請求（不分 dataset、不分 goroutine）共用同一個節流器。預設保守值：TWSE 與 TPEx 每 3–5 秒 1 個請求，並加隨機 jitter；實測後可調，但調整值要寫回本文件。
- **backfill 一律序列執行，不平行打同一來源**。缺口很大時寧可慢慢補（一個標的十年的月資料約 120 個請求，照預設速率約 10 分鐘），catch-up 機制本來就允許跨多個週期慢慢追平。
- **退避策略**：收到 429、403、連線重置或內容異常（如回應變成 HTML 錯誤頁）時，指數退避（基準 1 分鐘、上限 1 小時、加 jitter），連續失敗達上限就把該來源標記為「暫停抓取」並留下 `ingestion_runs` 紀錄，等下個排程週期再試——絕不在短時間內反覆重試。
- **進度可續傳**：backfill 以「標的 × 月份」為最小工作單位記錄完成狀態，中斷或被擋後從斷點繼續，不重抓已成功的部分。
- **原始回應落地**：每次成功抓取的原始 payload 先存檔（或存 `ingestion_runs` 關聯的 raw 資料）再解析。解析邏輯改版時重跑解析即可，不需要重新向來源請求。
- **避開壓力時段**：例行排程避開開盤時段與收盤後整點尖峰，安排在晚間離峰執行；backfill 大缺口優先安排在夜間。
- **誠實的 User-Agent**、不偽裝瀏覽器、不輪換 IP 規避封鎖。被擋表示太快，正確回應是降速，不是繞過。
- 若某 dataset 長期受限到無法實用，依優先序評估授權資料商，而不是加大抓取強度。

## 第一階段建議

先以台股日終資料、使用者手動交易流水與手動對帳為主，等 ledger 與建議引擎穩定後，再加入券商唯讀同步與近即時行情。
