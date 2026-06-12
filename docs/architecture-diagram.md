# 系統架構圖

這份圖表是目前文件整理後的架構版本。重點是：系統先做投資建議，不直接替使用者下單；庫存由交易流水推導；UI 與外部工具都走同一套 API。

## 系統總覽

```mermaid
flowchart TB
    user["使用者"] --> ui["Dashboard UI"]
    user --> ext["外部腳本 / 第三方工具"]

    ui --> api["API Server<br/>Go"]
    ext --> api

    api --> auth["Auth / API Key<br/>權限、撤銷、輪替"]
    api --> ledger["Portfolio Ledger<br/>交易流水與庫存計算"]
    api --> rec["Recommendation Engine<br/>投資建議引擎"]
    api --> settings["User Settings<br/>風險設定、策略設定"]
    api --> reconcile["Reconciliation<br/>券商對帳"]

    ingest["Market Data Ingestion<br/>資料匯入排程"] --> sources["台股資料來源<br/>TWSE / TPEx / MOPS / 授權資料商"]
    ingest --> market["Market Data Store<br/>行情、日 K、公司行動"]

    ledger --> snapshot["Portfolio Snapshot<br/>庫存、成本、現金流"]
    snapshot --> rec
    market --> rec
    settings --> rec

    rec --> runs["Recommendation Runs<br/>建議快照與明細"]
    runs --> api

    broker["Broker Connector<br/>未來：券商 API"] -.唯讀同步.-> brokerApi["券商 API"]
    broker --> brokerSnapshot["Broker Snapshot<br/>券商庫存與成交"]
    brokerSnapshot --> reconcile
    reconcile --> adjustment["Adjustment Event<br/>對帳調整事件"]
    adjustment --> ledger

    crypto["Crypto Connector<br/>未來：虛擬貨幣"] -.同步.-> cryptoEx["交易所 API"]
    crypto --> market

    db[("PostgreSQL")]
    auth --> db
    ledger --> db
    market --> db
    snapshot --> db
    settings --> db
    reconcile --> db
    runs --> db

    execution["Execution Gateway<br/>未來才可能啟用"] -.需要明確授權與風控.-> brokerApi
    runs -.採納後才可能送出.-> execution
```

## 建議產生流程

```mermaid
sequenceDiagram
    participant U as 使用者
    participant API as API Server
    participant L as Portfolio Ledger
    participant M as Market Data
    participant S as Strategy Core
    participant R as Recommendation Store

    U->>API: 新增買進 / 賣出 / 配息 / 調整事件
    API->>L: 寫入 ledger event
    L->>L: 重算庫存、批次成本、現金流
    API->>M: 讀取已入庫市場資料
    API->>L: 取得最新 portfolio snapshot
    API->>S: 傳入庫存、市場資料、策略參數、風險設定
    S-->>API: 回傳 recommendation intent
    API->>R: 保存 recommendation run 與明細
    API-->>U: 回傳建議、理由、資料時間與風險限制
```

## 券商對帳流程

```mermaid
flowchart LR
    brokerApi["券商 API / 匯入檔"] --> brokerSnapshot["Broker Snapshot"]
    ledgerSnapshot["系統 Portfolio Snapshot"] --> diff["差異比對"]
    brokerSnapshot --> diff
    diff --> report["Reconciliation Report<br/>股數、成本、現金、配息差異"]
    report --> review["使用者確認"]
    review --> adjustment["新增 Adjustment Event"]
    adjustment --> ledger["Portfolio Ledger"]
    ledger --> recalculated["重算庫存快照"]
```

## 邊界確認

- UI 不直接碰資料庫，只呼叫 API。
- 庫存不是 CRUD 表，而是由 ledger event 算出來。
- 策略核心只做計算，不抓網路、不讀寫資料庫、不寫檔案。
- 市場資料先入庫，建議引擎只吃已整理好的資料。
- 券商 API 第一階段只做唯讀同步與對帳。
- 自動下單不在第一階段；未來若要做，會放在獨立 Execution Gateway，並要求明確授權、風控與審計。
