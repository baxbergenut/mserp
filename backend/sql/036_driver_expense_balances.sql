BEGIN;

ALTER TABLE expenses DROP CONSTRAINT expenses_category_check;
ALTER TABLE expenses ADD CONSTRAINT expenses_category_check CHECK
    (category IN ('Maintenance', 'Other', 'Safety', 'HR', 'Administrative', 'Penalties'));
ALTER TABLE expenses ADD COLUMN driver_settled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE expenses ADD COLUMN balance_version INTEGER NOT NULL DEFAULT 1;
-- One-time opening balance: existing driver expenses have already been paid.
UPDATE expenses SET driver_settled = true WHERE lower(btrim(covered_by)) = 'driver';

CREATE TABLE expense_payments (
    expense_id UUID NOT NULL REFERENCES expenses(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (expense_id, week_start)
);
CREATE INDEX expense_payments_week_idx ON expense_payments(week_start);

-- Preserve the expense identity and payroll history, including from older clients.
CREATE FUNCTION protect_expense_balance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.balance_version := OLD.balance_version + 1;
        IF EXISTS (SELECT 1 FROM expense_payments WHERE expense_id = OLD.id)
           AND (NEW.driver_id IS DISTINCT FROM OLD.driver_id
             OR NEW.expense_date IS DISTINCT FROM OLD.expense_date
             OR NEW.amount IS DISTINCT FROM OLD.amount
             OR lower(btrim(NEW.covered_by)) IS DISTINCT FROM lower(btrim(OLD.covered_by))) THEN
            RAISE EXCEPTION 'An expense used in Driver Pay cannot change its driver, date, amount or responsibility' USING ERRCODE = '23514';
        END IF;
        IF OLD.driver_settled AND (NEW.amount IS DISTINCT FROM OLD.amount
             OR lower(btrim(NEW.covered_by)) IS DISTINCT FROM lower(btrim(OLD.covered_by))) THEN
            RAISE EXCEPTION 'Previously paid driver expenses retain their original amount and responsibility; create a new expense' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_balance_guard BEFORE UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION protect_expense_balance();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE expense_payments OWNER TO mserp_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON expenses TO mserp_app;
    END IF;
END $$;
COMMIT;
