BEGIN;

CREATE TABLE telegram_expense_update_expenses (
    update_id     BIGINT NOT NULL REFERENCES telegram_expense_updates(update_id) ON DELETE CASCADE,
    expense_index INTEGER NOT NULL CHECK (expense_index >= 0),
    expense_id    UUID NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (update_id, expense_index),
    UNIQUE (expense_id)
);

INSERT INTO telegram_expense_update_expenses (update_id, expense_index, expense_id)
SELECT update_id, 0, expense_id
FROM telegram_expense_updates
WHERE expense_id IS NOT NULL
ON CONFLICT DO NOTHING;

COMMIT;
