# 系統架構

架構圖請看 `docs/architecture-diagram.md`。

## 架構原則

Easy Invest 採 API-first 架構。所有功能都先由 API 提供，UI 只是同一套 API 的操作介面。這樣未來才容易接行動 App、自動化腳本、券商 connector 或第三方工具。

核心原則：

- 建議與下單分離。
- 庫存由流水推導，不直接覆寫。
- 策略核心純計算，不做 I/O。
- 市場資料先入庫，再進策略。
- 每次建議都能回放當時輸入。
- 台股優先，但資產模型保留擴充到虛擬貨幣的能力。

## 主要模組

### API Server

Go 後端服務，負責：

- 使用者登入與 API key 管理。
- 交易流水 API。
- 庫存查詢 API。
- 市場資料查詢 API。
- 投資建議 API。
- 對帳與調整事件 API。
- 系統設定 API。

UI 必須只透過 API 操作資料，不直接碰資料庫。

### Portfolio Ledger

負責保存交易流水與公司行動事件，並計算目前庫存、成本、已實現損益、未實現損益與現金流。

它不接受「直接把庫存改成 X」這種操作。若要修正，必須新增調整事件。

### Market Data

負責抓取、正規化與儲存市場資料。

第一階段以台股資料為主，資料來源優先使用官方與授權來源。每批資料都要保存來源、時間與匯入狀態。

### Recommendation Engine

負責把庫存快照、市場資料、策略參數與風險設定轉成投資建議。

輸出的是建議，不是券商委託單。即使未來支援自動下單，也應先產生建議，再由獨立的 execution 模組判斷是否能送單。

### Strategy / Backtest / Evolution

策略核心共用同一套計算邏輯。回測與正式建議的差別只在輸入資料來源與輸出保存方式。

參考文件中的 GA 進化、Sigmoid 目標權重與 Ghost DCA 基準可以作為策略研究方向，但要改成台股與投資建議語境。

### Broker Connector

未來模組，分階段支援：

- 讀取券商庫存。
- 讀取成交紀錄。
- 協助對帳。
- 在完整授權與風控後才可能送出委託。

券商 connector 不應污染核心庫存模型；它只提供外部事實，系統再轉成 ledger event 或 reconciliation result。

### Crypto Connector

未來模組，負責虛擬貨幣交易所資料與帳戶同步。核心模型要支援不同資產類型，但股票專屬概念不可硬塞到 crypto。

## 資料庫

使用 PostgreSQL。Docker Compose 至少包含：

- API server。
- PostgreSQL。
- 背景 worker 或排程服務。

Redis、message queue 或其他元件不是第一階段必備項目；只有在需求明確時才加入。

## 概念資料表

第一階段至少需要這些概念：

- `users`
- `api_keys`
- `assets`
- `market_data_sources`
- `market_bars`
- `ledger_events`
- `portfolio_snapshots`
- `broker_connections`
- `broker_snapshots`
- `reconciliation_runs`
- `recommendation_runs`
- `recommendation_items`
- `strategy_versions`

實際 schema 等開始寫程式前再定義。正式環境建議使用可審查的 migration 流程，不要照抄參考文件的 AutoMigrate-only 規則。

## 主要流程

### 新增交易

1. 使用者透過 UI 或 API 新增交易。
2. API 驗證股票代碼、日期、單位、價格、股數或張數。
3. 寫入 ledger event。
4. 系統重新計算庫存快照。
5. 下一次建議使用新的庫存狀態。

### 產生建議

1. 取得使用者最新庫存快照。
2. 取得已入庫市場資料。
3. 取得策略版本與使用者風險設定。
4. 呼叫策略核心。
5. 保存 recommendation run 與 recommendation items。
6. API 回傳建議與解釋。

### 券商對帳

1. 使用者匯入或同步券商庫存。
2. 系統保存 broker snapshot。
3. 系統比對內部庫存快照與券商 snapshot。
4. 產生差異報告。
5. 使用者確認後新增調整事件，不直接改歷史交易。

## 與參考文件的差異

參考文件偏向三端式 SaaS 自動交易，本專案先採較保守的設計：

- 不預設 LocalAgent。
- 不預設自動下單。
- 不把 TradeCommand 當核心輸出。
- 核心輸出改為 Recommendation。
- 先做好交易流水、成本對帳、資料來源與建議可解釋性。

這樣比較符合「系統給建議、使用者更新庫存」的使用方式，也保留未來券商 API 的擴充空間。
