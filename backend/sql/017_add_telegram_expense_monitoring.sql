BEGIN;

ALTER TABLE telegram_expense_updates
    DROP CONSTRAINT IF EXISTS telegram_expense_updates_status_check;

ALTER TABLE telegram_expense_updates
    ADD CONSTRAINT telegram_expense_updates_status_check CHECK (
        status IN ('queued', 'processing', 'retry', 'completed', 'ignored', 'needs_review', 'failed')
    ),
    ADD COLUMN extracted_data JSONB;

CREATE INDEX telegram_expense_updates_status_created_idx
    ON telegram_expense_updates (status, created_at DESC);

COMMIT;
