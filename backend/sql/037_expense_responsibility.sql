BEGIN;
ALTER TABLE expenses ADD COLUMN owner_id UUID REFERENCES investors(id);
ALTER TABLE expenses ADD COLUMN charge_driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL;
UPDATE expenses SET charge_driver_id=driver_id WHERE lower(btrim(covered_by))='driver';
-- Historical owner expenses have no reliable ownership snapshot. Do not infer
-- their debtor from today's assignment or introduce old payroll deductions.
UPDATE expenses SET driver_settled=true WHERE lower(btrim(covered_by))='truck owner';
CREATE INDEX expenses_charge_driver_idx ON expenses(charge_driver_id,expense_date);

CREATE FUNCTION resolve_expense_responsibility() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.owner_id IS NOT DISTINCT FROM OLD.owner_id
           AND NEW.driver_id IS NOT DISTINCT FROM OLD.driver_id
           AND NEW.covered_by IS NOT DISTINCT FROM OLD.covered_by THEN
            RETURN NEW;
        END IF;
        IF EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id)
           AND NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
            RAISE EXCEPTION 'An expense used in Driver Pay cannot change its owner' USING ERRCODE='23514';
        END IF;
    END IF;
    NEW.charge_driver_id := NULL;
    IF lower(btrim(NEW.covered_by))='driver' THEN
        NEW.owner_id := NULL;
        NEW.charge_driver_id := NEW.driver_id;
    ELSIF lower(btrim(NEW.covered_by))='truck owner' THEN
        IF NEW.owner_id IS NULL THEN
            RAISE EXCEPTION 'Select the responsible truck owner' USING ERRCODE='23514';
        END IF;
        SELECT driver_id INTO NEW.charge_driver_id FROM investors WHERE id=NEW.owner_id AND NOT is_company;
        IF NEW.owner_id='00000000-0000-0000-0000-000000000001' THEN
            NEW.covered_by := 'Company';
            NEW.owner_id := NULL;
        END IF;
    ELSE
        NEW.owner_id := NULL;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_00_responsibility BEFORE INSERT OR UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION resolve_expense_responsibility();
COMMIT;
