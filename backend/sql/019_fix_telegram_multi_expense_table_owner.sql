BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE telegram_expense_update_expenses OWNER TO mserp_app;
    END IF;
END
$$;

COMMIT;
