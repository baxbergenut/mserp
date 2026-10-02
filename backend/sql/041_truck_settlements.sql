BEGIN;

-- Terms are explicit, effective-dated agreements. No rate or historical owner
-- is inferred from a driver's current profile or a current truck assignment.
CREATE TABLE truck_settlement_terms (
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    owner_id UUID NOT NULL REFERENCES investors(id) ON DELETE RESTRICT,
    share_percent NUMERIC(7,4) NOT NULL CHECK (share_percent>0 AND share_percent<=100),
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(truck_id,week_start)
);
CREATE TABLE truck_charge_phases (
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    type_id UUID NOT NULL REFERENCES driver_charge_types(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(14,2) NOT NULL CHECK(amount>=0),
    included BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id),
    PRIMARY KEY(truck_id,type_id,week_start)
);
CREATE TABLE investor_pay_weeks (
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK(extract(isodow FROM week_start)=1),
    owner_id UUID NOT NULL REFERENCES investors(id) ON DELETE RESTRICT,
    edits JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    finalized BOOLEAN NOT NULL DEFAULT false,
    report JSONB,
    finalized_at TIMESTAMPTZ,
    finalized_by UUID REFERENCES app_users(id),
    PRIMARY KEY(truck_id,week_start)
);
CREATE TABLE truck_settlement_events (
    id BIGSERIAL PRIMARY KEY,
    truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL,
    action TEXT NOT NULL,
    details JSONB NOT NULL,
    actor_id UUID REFERENCES app_users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- A payment belongs to only one settlement. Existing driver payments keep
-- their original identity and continue to reserve the expense principal.
ALTER TABLE expense_payments ADD COLUMN investor_truck_id UUID REFERENCES trucks(id) ON DELETE RESTRICT;

CREATE FUNCTION protect_investor_expense_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tid uuid; wk date;
BEGIN
    IF TG_OP='DELETE' THEN tid:=OLD.investor_truck_id;wk:=OLD.week_start;
    ELSE tid:=NEW.investor_truck_id;wk:=NEW.week_start; END IF;
    IF TG_OP='INSERT' AND NEW.investor_truck_id IS NULL AND EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=NEW.expense_id AND week_start=NEW.week_start AND investor_truck_id IS NOT NULL) THEN
        RAISE EXCEPTION 'This expense payment belongs to Investor Pay' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND NEW.investor_truck_id IS DISTINCT FROM OLD.investor_truck_id THEN
        RAISE EXCEPTION 'A payment cannot change settlement destination' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=tid AND week_start=wk AND finalized) THEN
        RAISE EXCEPTION 'Reopen the investor settlement before editing its expenses' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER investor_expense_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_payments
FOR EACH ROW EXECUTE FUNCTION protect_investor_expense_payment();

CREATE OR REPLACE FUNCTION protect_finalized_payroll() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date;
BEGIN
    IF TG_TABLE_NAME='driver_pay_weeks' THEN
        IF TG_OP='DELETE' THEN d:=OLD.driver_id;w:=OLD.week_start; ELSE d:=NEW.driver_id;w:=NEW.week_start; END IF;
    ELSIF TG_TABLE_NAME='expense_payments' THEN
        IF TG_OP='DELETE' THEN
            IF OLD.investor_truck_id IS NOT NULL THEN RETURN OLD; END IF;
        ELSE
            IF NEW.investor_truck_id IS NOT NULL THEN RETURN NEW; END IF;
        END IF;
        IF TG_OP='DELETE' THEN
            SELECT charge_driver_id INTO d FROM expenses WHERE id=OLD.expense_id;w:=OLD.week_start;
        ELSE SELECT charge_driver_id INTO d FROM expenses WHERE id=NEW.expense_id;w:=NEW.week_start; END IF;
    ELSE
        IF TG_OP='DELETE' THEN
            SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=OLD.schedule_id;w:=OLD.week_start;
        ELSE SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=NEW.schedule_id;w:=NEW.week_start; END IF;
        -- Schedule maintenance may re-store an unchanged frozen occurrence.
        IF TG_OP='UPDATE' AND NEW.amount=OLD.amount AND NEW.name=OLD.name
           AND NEW.overridden=OLD.overridden AND NEW.confirmed_at IS NOT DISTINCT FROM OLD.confirmed_at THEN RETURN NEW; END IF;
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=d AND week_start=w AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing payroll' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION protect_truck_expense_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.owner_id IS DISTINCT FROM OLD.owner_id)
       AND EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id) THEN
        RAISE EXCEPTION 'An expense used in a settlement cannot change its truck or owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER truck_expense_identity_guard BEFORE UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION protect_truck_expense_identity();

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE truck_settlement_terms OWNER TO mserp_app;
        ALTER TABLE truck_charge_phases OWNER TO mserp_app;
        ALTER TABLE investor_pay_weeks OWNER TO mserp_app;
        ALTER TABLE truck_settlement_events OWNER TO mserp_app;
        ALTER SEQUENCE truck_settlement_events_id_seq OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
