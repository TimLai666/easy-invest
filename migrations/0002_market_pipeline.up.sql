-- 市場資料管線（M3）：backfill 斷點續傳與關注清單。

-- backfill 以「dataset × 標的 × 月份」為最小工作單位記錄進度，
-- 中斷或被來源限流後可從斷點繼續，不重抓已成功的部分。
CREATE TABLE backfill_checkpoints (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset    TEXT NOT NULL,
    symbol     TEXT NOT NULL,
    month      DATE NOT NULL, -- 該月份第一天
    status     TEXT NOT NULL DEFAULT 'pending', -- pending / succeeded / failed / unrecoverable
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (dataset, symbol, month)
);
CREATE INDEX backfill_checkpoints_status_idx ON backfill_checkpoints (dataset, status);

-- 使用者關注清單：與持有過的標的、基準標的一起構成日終行情的追蹤範圍。
CREATE TABLE watchlist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id),
    asset_id   UUID NOT NULL REFERENCES assets(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, asset_id)
);
CREATE INDEX watchlist_user_idx ON watchlist (user_id);
