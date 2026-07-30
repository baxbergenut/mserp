BEGIN;

ALTER TABLE tolls
    ALTER COLUMN report_id DROP NOT NULL,
    ALTER COLUMN truck_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS prepass_environment TEXT,
    ADD COLUMN IF NOT EXISTS prepass_toll_id BIGINT;

ALTER TABLE tolls
    ADD CONSTRAINT tolls_prepass_environment_check
    CHECK (
        prepass_environment IS NULL
        OR prepass_environment IN ('nonproduction', 'production')
    );

CREATE UNIQUE INDEX IF NOT EXISTS tolls_prepass_transaction_idx
    ON tolls (prepass_environment, prepass_toll_id);

CREATE TABLE IF NOT EXISTS prepass_toll_sync_days (
    prepass_environment TEXT NOT NULL
        CHECK (prepass_environment IN ('nonproduction', 'production')),
    sync_date          DATE NOT NULL,
    transaction_count INTEGER NOT NULL CHECK (transaction_count >= 0),
    fetched_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (prepass_environment, sync_date)
);

COMMIT;
