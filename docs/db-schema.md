# 資料庫 Schema 草案（v0）

PostgreSQL 16+。這份是進入 M0/M1 時建立 migration 的依據；實作時允許微調欄位，但表的職責劃分與「庫存由事件推導」的不變式不可改。每次調整 schema 必須同步更新本文件。

慣例：

- 主鍵一律 `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`。
- 時間戳 `timestamptz`，預設 `now()`；「日」概念用 `date`（Asia/Taipei 語意）。
- 金額 `NUMERIC(20,6)`，數量 `NUMERIC(28,10)`（台股為整數，小數留給未來 crypto）。
- 所有屬於使用者的表都有 `user_id` 外鍵，查詢一律帶 user 條件（單機自用也照做，避免未來多使用者時翻修）。
- 不做硬刪除的表：`ledger_events`、`recommendation_runs`、`ingestion_runs`。修正用 void/調整事件。

## 身分與授權

```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         CITEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,              -- argon2id
    display_name  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at   TIMESTAMPTZ
);

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    key_prefix   TEXT NOT NULL,               -- 明文前 8 碼，供 UI 辨識
    key_hash     TEXT NOT NULL,               -- SHA-256(完整 key)
    scopes       TEXT[] NOT NULL,             -- 見 api-spec.md scope 清單
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX ON api_keys (user_id);
CREATE UNIQUE INDEX ON api_keys (key_hash);
```

## 資產主檔

```sql
CREATE TABLE assets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_type TEXT NOT NULL,        -- 'tw_stock' | 'tw_etf' | 'tw_bond_etf' | 未來 'crypto'
    symbol     TEXT NOT NULL,        -- '2330', '0050'；crypto 用交易對
    name       TEXT NOT NULL,
    market     TEXT NOT NULL,        -- 'TWSE' | 'TPEX' | 未來交易所代碼
    currency   TEXT NOT NULL DEFAULT 'TWD',
    lot_size   INTEGER NOT NULL DEFAULT 1000,  -- 台股 1 張 = 1000 股；crypto 為 1
    is_active  BOOLEAN NOT NULL DEFAULT true,
    metadata   JSONB NOT NULL DEFAULT '{}',    -- 股票專屬欄位放這裡，不污染核心欄位
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (market, symbol)
);
```

## 市場資料

```sql
CREATE TABLE ingestion_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_name    TEXT NOT NULL,      -- 'twse_openapi' 等
    source_url     TEXT NOT NULL,
    source_license TEXT NOT NULL DEFAULT '',
    dataset        TEXT NOT NULL,      -- 'daily_bars' | 'corporate_actions' | 'calendar' | 'asset_list'
    fetched_at     TIMESTAMPTZ NOT NULL,
    data_time      TIMESTAMPTZ,        -- 資料所屬時間
    status         TEXT NOT NULL,      -- 'running' | 'succeeded' | 'failed' | 'partial'
    row_count      INTEGER,
    checksum       TEXT,
    error_message  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE market_daily_bars (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id         UUID NOT NULL REFERENCES assets(id),
    bar_date         DATE NOT NULL,
    open             NUMERIC(20,6),
    high             NUMERIC(20,6),
    low              NUMERIC(20,6),
    close            NUMERIC(20,6) NOT NULL,
    volume_shares    NUMERIC(28,10),
    turnover         NUMERIC(20,2),
    ingestion_run_id UUID NOT NULL REFERENCES ingestion_runs(id),
    revision         INTEGER NOT NULL DEFAULT 0,   -- 來源修正時 +1，舊列保留
    is_latest        BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ON market_daily_bars (asset_id, bar_date, revision);
CREATE INDEX ON market_daily_bars (asset_id, bar_date) WHERE is_latest;

CREATE TABLE corporate_actions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id         UUID NOT NULL REFERENCES assets(id),
    action_type      TEXT NOT NULL,    -- 'cash_dividend' | 'stock_dividend' | 'split'
                                       -- | 'capital_reduction' | 'rights_issue'
    ex_date          DATE NOT NULL,
    record_date      DATE,
    pay_date         DATE,
    cash_per_share   NUMERIC(20,6),    -- 現金股利 / 減資退還金額
    stock_ratio      NUMERIC(20,10),   -- 配股率 r / 分割比例 / 減資換股比例
    details          JSONB NOT NULL DEFAULT '{}',
    ingestion_run_id UUID REFERENCES ingestion_runs(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (asset_id, action_type, ex_date)
);

CREATE TABLE trading_calendar (
    market  TEXT NOT NULL,
    cal_date DATE NOT NULL,
    is_open BOOLEAN NOT NULL,
    note    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (market, cal_date)
);
```

## Ledger（核心）

```sql
CREATE TABLE ledger_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id),
    asset_id          UUID REFERENCES assets(id),   -- 純現金事件可為 NULL
    event_type        TEXT NOT NULL,
    -- 'buy' | 'sell' | 'cash_dividend' | 'stock_dividend' | 'split'
    -- | 'capital_reduction' | 'cash_deposit' | 'cash_withdraw'
    -- | 'fee_adjustment' | 'tax_adjustment'
    -- | 'broker_reconciliation_adjustment' | 'manual_correction'
    trade_date        DATE NOT NULL,
    settlement_date   DATE,
    quantity_shares   NUMERIC(28,10),     -- 正=增加持股，負=減少
    price             NUMERIC(20,6),
    gross_amount      NUMERIC(20,6),      -- 價金或股利毛額，恆為正
    fee               NUMERIC(20,6) NOT NULL DEFAULT 0,
    tax               NUMERIC(20,6) NOT NULL DEFAULT 0,
    cash_delta        NUMERIC(20,6) NOT NULL DEFAULT 0,  -- 對現金部位的淨影響（含正負）
    currency          TEXT NOT NULL DEFAULT 'TWD',
    fee_source        TEXT NOT NULL DEFAULT 'user',      -- 'user' | 'estimated'
    lot_id            UUID,               -- specific-lot 賣出時指定（第二階段）
    source            TEXT NOT NULL DEFAULT 'manual',    -- 'manual' | 'broker_import' | 'reconciliation'
    source_ref        TEXT,               -- 券商成交序號、匯入檔列號等
    reconciliation_run_id UUID,
    notes             TEXT NOT NULL DEFAULT '',
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    voided_at         TIMESTAMPTZ,
    void_reason       TEXT,
    reverses_event_id UUID REFERENCES ledger_events(id)  -- 反向沖銷對象
);
CREATE INDEX ON ledger_events (user_id, asset_id, trade_date);
CREATE INDEX ON ledger_events (user_id, trade_date);
```

不變式（程式與測試都要守）：

- 事件建立後不可 UPDATE 業務欄位；只允許設定 `voided_at` / `void_reason`。
- void 一筆事件必須觸發相關批次與快照重算。
- `cash_delta` 由服務層依事件類型計算寫入，例如買進 = −(gross + fee)、賣出 = gross − fee − tax、現金股利 = gross − fee − tax。

## 批次（lots）

批次是由 buy/stock_dividend 等事件「物化」出來的衍生資料，跟事件同交易（transaction）內維護；任何事件 void 後從事件流重建該資產的批次。

```sql
CREATE TABLE lots (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id),
    asset_id           UUID NOT NULL REFERENCES assets(id),
    opened_by_event_id UUID NOT NULL REFERENCES ledger_events(id),
    open_date          DATE NOT NULL,
    original_quantity  NUMERIC(28,10) NOT NULL,
    remaining_quantity NUMERIC(28,10) NOT NULL,
    original_cost      NUMERIC(20,6) NOT NULL,   -- 含買進手續費的總成本
    closed_at          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON lots (user_id, asset_id) WHERE closed_at IS NULL;

CREATE TABLE lot_consumptions (        -- 賣出/減資沖銷明細
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lot_id            UUID NOT NULL REFERENCES lots(id),
    consumed_by_event_id UUID NOT NULL REFERENCES ledger_events(id),
    quantity          NUMERIC(28,10) NOT NULL,
    cost_consumed     NUMERIC(20,6) NOT NULL,
    realized_pnl      NUMERIC(20,6),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 快照

```sql
CREATE TABLE portfolio_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    as_of       TIMESTAMPTZ NOT NULL,
    kind        TEXT NOT NULL,          -- 'latest' | 'recommendation_input' | 'daily_close'
    positions   JSONB NOT NULL,         -- [{asset_id, symbol, quantity, lots:[...], avg_cost, ...}]
    cash        NUMERIC(20,6) NOT NULL,
    is_stale    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON portfolio_snapshots (user_id, kind, as_of DESC);
```

## 設定與策略

```sql
CREATE TABLE user_settings (
    user_id            UUID PRIMARY KEY REFERENCES users(id),
    fee_rate           NUMERIC(10,8) NOT NULL DEFAULT 0.001425,
    fee_discount       NUMERIC(6,4)  NOT NULL DEFAULT 1.0,
    fee_minimum        NUMERIC(10,2) NOT NULL DEFAULT 20,
    dividend_transfer_fee NUMERIC(10,2) NOT NULL DEFAULT 10,
    cash_buffer        NUMERIC(20,6) NOT NULL DEFAULT 0,     -- 建議引擎不得動用的現金
    min_trade_amount   NUMERIC(20,6) NOT NULL DEFAULT 10000, -- 低於此金額不出建議
    prefer_whole_lot   BOOLEAN NOT NULL DEFAULT false,       -- 建議以整張為單位
    risk_profile       TEXT NOT NULL DEFAULT 'balanced',     -- 'conservative'|'balanced'|'aggressive'
    target_weights     JSONB NOT NULL DEFAULT '{}',          -- {"2330": 0.4, "0050": 0.4, "cash": 0.2}
    rebalance_band     NUMERIC(6,4) NOT NULL DEFAULT 0.05,   -- 偏離目標 ±5% 才觸發建議
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE strategy_versions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,           -- 'target_weight_rebalance'
    version     TEXT NOT NULL,           -- semver，策略邏輯有變就 bump
    params_schema JSONB NOT NULL DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);
```

## 建議

```sql
CREATE TABLE recommendation_runs (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               UUID NOT NULL REFERENCES users(id),
    strategy_version_id   UUID NOT NULL REFERENCES strategy_versions(id),
    portfolio_snapshot_id UUID NOT NULL REFERENCES portfolio_snapshots(id),
    market_data_as_of     DATE NOT NULL,        -- 使用的收盤資料日期
    inputs                JSONB NOT NULL,       -- 策略參數、風險設定的完整快照（可回放）
    status                TEXT NOT NULL DEFAULT 'completed',  -- 'completed' | 'failed'
    error_message         TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON recommendation_runs (user_id, created_at DESC);

CREATE TABLE recommendation_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES recommendation_runs(id),
    asset_id        UUID REFERENCES assets(id),   -- NULL = 現金部位建議
    action          TEXT NOT NULL,    -- 'buy' | 'sell' | 'hold' | 'no_action'
    quantity_shares NUMERIC(28,10),
    est_price       NUMERIC(20,6),
    est_amount      NUMERIC(20,6),
    est_fee_tax     NUMERIC(20,6),
    current_weight  NUMERIC(8,6),
    target_weight   NUMERIC(8,6),
    reason          TEXT NOT NULL,    -- 人話解釋，UI 直接顯示
    risks           TEXT NOT NULL DEFAULT '',
    confidence      NUMERIC(4,3),     -- 0~1，v1 可固定值
    user_status     TEXT NOT NULL DEFAULT 'draft',
    -- 'draft' | 'viewed' | 'accepted' | 'ignored' | 'expired'
    status_changed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON recommendation_items (run_id);
```

## 對帳

```sql
CREATE TABLE broker_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    broker_name TEXT NOT NULL,         -- '永豐' '富邦' 等，自由文字
    source      TEXT NOT NULL DEFAULT 'csv_import',  -- 未來 'api'
    captured_at TIMESTAMPTZ NOT NULL,  -- 券商資料時點
    raw_payload JSONB NOT NULL,        -- 原始匯入內容，永久保留
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE broker_positions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id   UUID NOT NULL REFERENCES broker_snapshots(id),
    symbol        TEXT NOT NULL,
    quantity_shares NUMERIC(28,10) NOT NULL,
    broker_avg_cost NUMERIC(20,6),
    details       JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE reconciliation_runs (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id),
    broker_snapshot_id UUID NOT NULL REFERENCES broker_snapshots(id),
    status             TEXT NOT NULL DEFAULT 'open',  -- 'open' | 'resolved' | 'dismissed'
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE reconciliation_diffs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id              UUID NOT NULL REFERENCES reconciliation_runs(id),
    asset_id            UUID REFERENCES assets(id),
    diff_type           TEXT NOT NULL,
    -- 'quantity_mismatch' | 'avg_cost_mismatch' | 'missing_dividend'
    -- | 'missing_position_internal' | 'missing_position_broker' | 'fee_tax_mismatch'
    internal_value      TEXT,
    broker_value        TEXT,
    resolution          TEXT NOT NULL DEFAULT 'pending',
    -- 'pending' | 'adjusted' | 'accepted_as_is' | 'fixed_input'
    adjustment_event_id UUID REFERENCES ledger_events(id),
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 稽核

```sql
CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID,
    api_key_id UUID,
    action     TEXT NOT NULL,      -- 'ledger.create' 'apikey.revoke' 等
    entity     TEXT,
    entity_id  UUID,
    detail     JSONB NOT NULL DEFAULT '{}',
    ip         INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON audit_log (user_id, created_at DESC);
```

## Migration 紀律

- 工具：`golang-migrate`，純 SQL up/down 檔，存放於 `migrations/`（以 `embed.FS` 打包進執行檔），檔名 `NNNN_description.up.sql`。
- 禁止任何 ORM AutoMigrate；schema 變更一律走可審查的 SQL migration 檔。
- **程式啟動時自動執行 migration**：API server 與 worker 啟動時先跑 migrate up 到最新版，成功才開始服務。多個程序同時啟動時，以 PostgreSQL advisory lock 防止並發執行——搶到鎖的程序執行，其餘等待後驗證版本一致再繼續。
- migration 失敗（含 dirty state）時程序直接結束並輸出明確錯誤，不得帶著不一致的 schema 提供服務；dirty state 的修復用 `cmd/migrate` 手動處理。
- `cmd/migrate` 保留為輔助工具：查版本、force、down、開發期重建。
- CI 驗證 migration 可在乾淨資料庫從 0 跑到最新。
- down migration 只求開發期可用；生產回滾以「新增修正 migration」為原則。新增 migration 必須與當下程式版本相容（先加欄位後用、不在同一版直接刪改線上仍在用的欄位），因為部署即自動套用。
