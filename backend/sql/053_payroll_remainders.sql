BEGIN;
ALTER TABLE driver_charge_occurrences ADD COLUMN base_amount NUMERIC(14,2);
ALTER TABLE driver_charge_occurrences ADD COLUMN waive_remainder BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE driver_pay_cost_collections (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    fuel_base NUMERIC(14,2) NOT NULL,
    toll_base NUMERIC(14,2) NOT NULL,
    fuel_amount NUMERIC(14,2) NOT NULL,
    toll_amount NUMERIC(14,2) NOT NULL,
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(driver_id,week_start)
);

CREATE FUNCTION protect_payroll_remainders() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date;
BEGIN
    IF TG_TABLE_NAME='driver_pay_cost_collections' THEN
        IF TG_OP='DELETE' THEN d:=OLD.driver_id; w:=OLD.week_start;
        ELSE d:=NEW.driver_id; w:=NEW.week_start; END IF;
    ELSE
        IF TG_OP<>'UPDATE' OR (NEW.base_amount IS NOT DISTINCT FROM OLD.base_amount AND NEW.waive_remainder=OLD.waive_remainder) THEN RETURN NEW; END IF;
        SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=NEW.schedule_id;
        w:=NEW.week_start;
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=d AND week_start=w AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing payroll' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER payroll_cost_collection_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_cost_collections FOR EACH ROW EXECUTE FUNCTION protect_payroll_remainders();
CREATE TRIGGER payroll_charge_remainder_guard BEFORE UPDATE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION protect_payroll_remainders();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_pay_cost_collections OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
