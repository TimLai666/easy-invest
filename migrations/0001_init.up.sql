CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         CITEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at   TIMESTAMPTZ
);

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    key_prefix   TEXT NOT NULL,
    key_hash     TEXT NOT NULL,
    scopes       TEXT[] NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX api_keys_user_id_idx ON api_keys (user_id);
CREATE UNIQUE INDEX api_keys_key_hash_idx ON api_keys (key_hash);

CREATE TABLE assets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_type TEXT NOT NULL,
    symbol     TEXT NOT NULL,
    name       TEXT NOT NULL,
    market     TEXT NOT NULL,
    currency   TEXT NOT NULL DEFAULT 'TWD',
    lot_size   INTEGER NOT NULL DEFAULT 1000,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    metadata   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (market, symbol)
);

CREATE TABLE ingestion_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_name    TEXT NOT NULL,
    source_url     TEXT NOT NULL,
    source_license TEXT NOT NULL DEFAULT '',
    dataset        TEXT NOT NULL,
    fetched_at     TIMESTAMPTZ NOT NULL,
    data_time      TIMESTAMPTZ,
    status         TEXT NOT NULL,
    row_count      INTEGER,
    checksum       TEXT,
    error_message  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ingestion_runs_dataset_created_idx ON ingestion_runs (dataset, created_at DESC);

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
    revision         INTEGER NOT NULL DEFAULT 0,
    is_latest        BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX market_daily_bars_asset_date_revision_idx ON market_daily_bars (asset_id, bar_date, revision);
CREATE INDEX market_daily_bars_latest_idx ON market_daily_bars (asset_id, bar_date) WHERE is_latest;

CREATE TABLE corporate_actions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id         UUID NOT NULL REFERENCES assets(id),
    action_type      TEXT NOT NULL,
    ex_date          DATE NOT NULL,
    record_date      DATE,
    pay_date         DATE,
    cash_per_share   NUMERIC(20,6),
    stock_ratio      NUMERIC(20,10),
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

CREATE TABLE ledger_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id),
    asset_id          UUID REFERENCES assets(id),
    event_type        TEXT NOT NULL,
    trade_date        DATE NOT NULL,
    settlement_date   DATE,
    quantity_shares   NUMERIC(28,10),
    price             NUMERIC(20,6),
    gross_amount      NUMERIC(20,6),
    fee               NUMERIC(20,6) NOT NULL DEFAULT 0,
    tax               NUMERIC(20,6) NOT NULL DEFAULT 0,
    cash_delta        NUMERIC(20,6) NOT NULL DEFAULT 0,
    currency          TEXT NOT NULL DEFAULT 'TWD',
    fee_source        TEXT NOT NULL DEFAULT 'user',
    lot_id            UUID,
    source            TEXT NOT NULL DEFAULT 'manual',
    source_ref        TEXT,
    reconciliation_run_id UUID,
    notes             TEXT NOT NULL DEFAULT '',
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    voided_at         TIMESTAMPTZ,
    void_reason       TEXT,
    reverses_event_id UUID REFERENCES ledger_events(id)
);
CREATE INDEX ledger_events_user_asset_date_idx ON ledger_events (user_id, asset_id, trade_date);
CREATE INDEX ledger_events_user_date_idx ON ledger_events (user_id, trade_date);

CREATE TABLE lots (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id),
    asset_id           UUID NOT NULL REFERENCES assets(id),
    opened_by_event_id UUID NOT NULL REFERENCES ledger_events(id),
    open_date          DATE NOT NULL,
    original_quantity  NUMERIC(28,10) NOT NULL,
    remaining_quantity NUMERIC(28,10) NOT NULL,
    original_cost      NUMERIC(20,6) NOT NULL,
    adjusted_cost      NUMERIC(20,6) NOT NULL,
    closed_at          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lots_user_asset_open_idx ON lots (user_id, asset_id) WHERE closed_at IS NULL;

CREATE TABLE lot_consumptions (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lot_id               UUID NOT NULL REFERENCES lots(id),
    consumed_by_event_id UUID NOT NULL REFERENCES ledger_events(id),
    quantity             NUMERIC(28,10) NOT NULL,
    cost_consumed        NUMERIC(20,6) NOT NULL,
    realized_pnl         NUMERIC(20,6),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE portfolio_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    as_of       TIMESTAMPTZ NOT NULL,
    kind        TEXT NOT NULL,
    positions   JSONB NOT NULL,
    cash        NUMERIC(20,6) NOT NULL,
    is_stale    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX portfolio_snapshots_user_kind_asof_idx ON portfolio_snapshots (user_id, kind, as_of DESC);

CREATE TABLE user_settings (
    user_id            UUID PRIMARY KEY REFERENCES users(id),
    fee_rate           NUMERIC(10,8) NOT NULL DEFAULT 0.001425,
    fee_discount       NUMERIC(6,4)  NOT NULL DEFAULT 1.0,
    fee_minimum        NUMERIC(10,2) NOT NULL DEFAULT 20,
    dividend_transfer_fee NUMERIC(10,2) NOT NULL DEFAULT 10,
    cash_buffer        NUMERIC(20,6) NOT NULL DEFAULT 0,
    min_trade_amount   NUMERIC(20,6) NOT NULL DEFAULT 10000,
    prefer_whole_lot   BOOLEAN NOT NULL DEFAULT false,
    risk_profile       TEXT NOT NULL DEFAULT 'balanced',
    target_weights     JSONB NOT NULL DEFAULT '{}',
    rebalance_band     NUMERIC(6,4) NOT NULL DEFAULT 0.05,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE strategy_versions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    version     TEXT NOT NULL,
    params_schema JSONB NOT NULL DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

CREATE TABLE recommendation_runs (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               UUID NOT NULL REFERENCES users(id),
    strategy_version_id   UUID NOT NULL REFERENCES strategy_versions(id),
    portfolio_snapshot_id UUID NOT NULL REFERENCES portfolio_snapshots(id),
    market_data_as_of     DATE NOT NULL,
    inputs                JSONB NOT NULL,
    status                TEXT NOT NULL DEFAULT 'completed',
    error_message         TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX recommendation_runs_user_created_idx ON recommendation_runs (user_id, created_at DESC);

CREATE TABLE recommendation_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES recommendation_runs(id),
    asset_id        UUID REFERENCES assets(id),
    action          TEXT NOT NULL,
    quantity_shares NUMERIC(28,10),
    est_price       NUMERIC(20,6),
    est_amount      NUMERIC(20,6),
    est_fee_tax     NUMERIC(20,6),
    current_weight  NUMERIC(8,6),
    target_weight   NUMERIC(8,6),
    reason          TEXT NOT NULL,
    risks           TEXT NOT NULL DEFAULT '',
    confidence      NUMERIC(4,3),
    user_status     TEXT NOT NULL DEFAULT 'draft',
    status_changed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX recommendation_items_run_idx ON recommendation_items (run_id);

CREATE TABLE broker_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    broker_name TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT 'csv_import',
    captured_at TIMESTAMPTZ NOT NULL,
    raw_payload JSONB NOT NULL,
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
    status             TEXT NOT NULL DEFAULT 'open',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE reconciliation_diffs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id              UUID NOT NULL REFERENCES reconciliation_runs(id),
    asset_id            UUID REFERENCES assets(id),
    diff_type           TEXT NOT NULL,
    internal_value      TEXT,
    broker_value        TEXT,
    resolution          TEXT NOT NULL DEFAULT 'pending',
    adjustment_event_id UUID REFERENCES ledger_events(id),
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID,
    api_key_id UUID,
    action     TEXT NOT NULL,
    entity     TEXT,
    entity_id  UUID,
    detail     JSONB NOT NULL DEFAULT '{}',
    ip         INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_user_created_idx ON audit_log (user_id, created_at DESC);

CREATE TABLE idempotency_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    idem_key    TEXT NOT NULL,
    method      TEXT NOT NULL,
    path        TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    response    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, idem_key, method, path)
);

INSERT INTO strategy_versions (name, version, description)
VALUES ('target_weight_rebalance', '1.0.0', '目標權重再平衡策略 v1')
ON CONFLICT (name, version) DO NOTHING;

INSERT INTO assets (asset_type, symbol, name, market, currency, lot_size, metadata)
VALUES
    ('tw_stock', '2330', '台積電', 'TWSE', 'TWD', 1000, '{"industry":"半導體"}'),
    ('tw_etf', '0050', '元大台灣50', 'TWSE', 'TWD', 1000, '{"category":"ETF"}'),
    ('tw_etf', '00878', '國泰永續高股息', 'TWSE', 'TWD', 1000, '{"category":"ETF"}'),
    ('tw_bond_etf', '00679B', '元大美債20年', 'TWSE', 'TWD', 1000, '{"category":"債券ETF"}')
ON CONFLICT (market, symbol) DO NOTHING;

