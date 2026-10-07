BEGIN;

-- Retain the original IDs and a full source snapshot for imported balances and
-- frozen payroll reports. Escrow principal and collections now have their own ledger.
CREATE TABLE driver_escrows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    driver_name TEXT NOT NULL,
    start_date DATE NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    opening_paid NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK (opening_paid >= 0 AND opening_paid <= amount),
    balance_version INTEGER NOT NULL DEFAULT 1,
    created_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    legacy_expense JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_escrows_driver_idx ON driver_escrows(driver_id);
CREATE TABLE driver_escrow_payments (
    escrow_id UUID NOT NULL REFERENCES driver_escrows(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(escrow_id,week_start)
);
CREATE INDEX driver_escrow_payments_week_idx ON driver_escrow_payments(week_start);

-- Include older manually entered escrow records, not only the system-tagged
-- onboarding entries. Settled imports keep their paid opening balance.
LOCK TABLE expenses, expense_payments IN ACCESS EXCLUSIVE MODE;
INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid,
    balance_version,created_by,created_at,updated_at,legacy_expense)
SELECT e.id,coalesce(e.charge_driver_id,e.driver_id),coalesce(e.driver_name,'Unknown driver'),
    greatest(coalesce(d.hire_date,DATE '2026-09-28'),DATE '2026-09-28'),coalesce(e.amount,0),
    CASE WHEN e.driver_settled THEN greatest(0,coalesce(e.amount,0)-coalesce(p.paid,0)) ELSE 0 END,
    e.balance_version,e.created_by,e.created_at,e.updated_at,to_jsonb(e)
FROM expenses e
LEFT JOIN drivers d ON d.id=coalesce(e.charge_driver_id,e.driver_id)
LEFT JOIN LATERAL (SELECT sum(amount) paid FROM expense_payments WHERE expense_id=e.id) p ON true
WHERE e.system_kind='driver_escrow'
   OR (lower(btrim(coalesce(e.expense_type,''))) IN ('escrow','escrow payment')
       AND lower(btrim(coalesce(e.covered_by,'')))='driver');
INSERT INTO driver_escrow_payments(escrow_id,week_start,amount,updated_by,updated_at)
SELECT p.expense_id,p.week_start,p.amount,p.updated_by,p.updated_at
FROM expense_payments p JOIN driver_escrows e ON e.id=p.expense_id;

-- This is a ledger transfer, not a payroll correction. Frozen report JSON and
-- audit snapshots remain untouched. The locks prevent writes during transfer.
ALTER TABLE expense_payments DISABLE TRIGGER USER;
DELETE FROM expense_payments p USING driver_escrows e WHERE p.expense_id=e.id;
ALTER TABLE expense_payments ENABLE TRIGGER USER;
DELETE FROM expenses e USING driver_escrows s WHERE e.id=s.id;

-- Keep the old column for schema compatibility, but it no longer controls escrow.
ALTER TABLE driver_escrow_settings ALTER COLUMN category_id DROP NOT NULL;
UPDATE driver_escrow_settings SET category_id=NULL;
UPDATE expense_settings SET active=false,version=version+1
WHERE kind='name' AND lower(btrim(name)) IN ('escrow','escrow payment');

CREATE FUNCTION protect_escrow_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e driver_escrows%ROWTYPE; target uuid; target_week date; paid numeric;
BEGIN
    IF TG_OP='DELETE' THEN target:=OLD.escrow_id; target_week:=OLD.week_start;
    ELSE target:=NEW.escrow_id; target_week:=NEW.week_start; END IF;
    IF TG_OP='UPDATE' AND (NEW.escrow_id<>OLD.escrow_id OR NEW.week_start<>OLD.week_start) THEN
        RAISE EXCEPTION 'Escrow payment identity cannot change' USING ERRCODE='23514';
    END IF;
    SELECT * INTO e FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=e.driver_id AND week_start=target_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing escrow' USING ERRCODE='23514';
    END IF;
    IF TG_OP<>'DELETE' THEN
        SELECT coalesce(sum(amount),0) INTO paid FROM driver_escrow_payments WHERE escrow_id=target AND week_start<>target_week;
        IF NEW.amount+paid+e.opening_paid>e.amount OR (target_week+6<e.start_date AND NOT EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id=target AND week_start=target_week)) THEN
            RAISE EXCEPTION 'Escrow payment exceeds the available balance or precedes its start' USING ERRCODE='23514';
        END IF;
    END IF;
    UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=target;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER escrow_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_payments
FOR EACH ROW EXECUTE FUNCTION protect_escrow_payment();

-- Older application releases must not recreate expense-backed escrow after the
-- transfer. Failing their transaction is safer than creating a second balance.
CREATE FUNCTION prevent_legacy_escrow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.system_kind='driver_escrow' OR (lower(btrim(coalesce(NEW.expense_type,''))) IN ('escrow','escrow payment') AND lower(btrim(coalesce(NEW.covered_by,'')))='driver') THEN
        RAISE EXCEPTION 'Use Accounting Escrow for driver escrow balances' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_legacy_escrow_guard BEFORE INSERT OR UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION prevent_legacy_escrow();

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_escrows OWNER TO mserp_app;
        ALTER TABLE driver_escrow_payments OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
