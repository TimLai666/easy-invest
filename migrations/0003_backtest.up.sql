-- 回測紀錄：params 保存輸入參數、result 保存可重現的回測輸出（皆為 JSONB）。
CREATE TABLE backtest_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id),
    name          TEXT NOT NULL DEFAULT '',
    params        JSONB NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'pending',
    result        JSONB,
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX backtest_runs_user_created_idx ON backtest_runs (user_id, created_at DESC);
