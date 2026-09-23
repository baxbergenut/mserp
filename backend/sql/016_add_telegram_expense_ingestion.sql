BEGIN;

CREATE TABLE telegram_expense_updates (
    update_id       BIGINT PRIMARY KEY,
    chat_id         BIGINT NOT NULL,
    message_id      BIGINT NOT NULL,
    chat_type       TEXT NOT NULL CHECK (chat_type IN ('group', 'supergroup')),
    raw_update      JSONB NOT NULL,
    status          TEXT NOT NULL DEFAULT 'queued' CHECK (
        status IN ('queued', 'processing', 'retry', 'completed', 'ignored', 'failed')
    ),
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    last_error      TEXT,
    expense_id      UUID REFERENCES expenses(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (chat_id, message_id)
);

CREATE INDEX telegram_expense_updates_work_idx
    ON telegram_expense_updates (next_attempt_at, update_id)
    WHERE status IN ('queued', 'retry', 'processing');
CREATE INDEX telegram_expense_updates_expense_idx
    ON telegram_expense_updates (expense_id)
    WHERE expense_id IS NOT NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE telegram_expense_updates OWNER TO mserp_app;
    END IF;
END
$$;

COMMIT;
