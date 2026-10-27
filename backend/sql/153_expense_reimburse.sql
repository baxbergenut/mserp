BEGIN;

-- Keep the existing setting identity and historical expense text.
INSERT INTO expense_settings(kind,name) VALUES('payment_method','Reimburse') ON CONFLICT DO NOTHING;
UPDATE expense_settings SET name='Reimburse',active=true,version=version+1
WHERE kind='payment_method' AND lower(name)='reimburse';

CREATE FUNCTION protect_reimburse_payment_method() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.kind='payment_method' AND lower(btrim(OLD.name))='reimburse' THEN
        RAISE EXCEPTION 'Reimburse is a permanent payment method and cannot be edited' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_settings_protect_reimburse BEFORE UPDATE OR DELETE ON expense_settings
FOR EACH ROW EXECUTE FUNCTION protect_reimburse_payment_method();

-- Credits are source-derived, so protect both their old and new statement week.
-- Serialize with finalization before it takes its repeatable-read snapshot.
CREATE FUNCTION protect_expense_reimbursement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_credit boolean := false; new_credit boolean := false;
BEGIN
    IF TG_OP<>'INSERT' THEN old_credit := lower(btrim(coalesce(OLD.payment_type,'')))='reimburse'; END IF;
    IF TG_OP<>'DELETE' THEN new_credit := lower(btrim(coalesce(NEW.payment_type,'')))='reimburse'; END IF;
    IF NOT old_credit AND NOT new_credit THEN
        IF TG_OP='DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' AND
       (NEW.driver_id,NEW.expense_date,NEW.amount,NEW.payment_type,NEW.expense_type,NEW.category)
       IS NOT DISTINCT FROM
       (OLD.driver_id,OLD.expense_date,OLD.amount,OLD.payment_type,OLD.expense_type,OLD.category) THEN RETURN NEW; END IF;
    PERFORM pg_advisory_xact_lock(736281940);
    IF TG_OP<>'INSERT' AND old_credit AND EXISTS(
        SELECT 1 FROM payroll_settlements WHERE finalized
        AND driver_id=payroll_driver_at(OLD.driver_id,OLD.expense_date)
        AND week_start=date_trunc('week',OLD.expense_date)::date
    ) THEN
        RAISE EXCEPTION 'Reopen the driver statement before changing its reimbursement' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
    END IF;
    IF TG_OP<>'DELETE' AND new_credit THEN
        IF NEW.driver_id IS NULL OR NEW.amount IS NULL OR NEW.amount<0 THEN
            RAISE EXCEPTION 'Reimburse requires a linked driver and a nonnegative amount' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
        END IF;
        IF EXISTS(SELECT 1 FROM payroll_settlements WHERE finalized
            AND driver_id=payroll_driver_at(NEW.driver_id,NEW.expense_date)
            AND week_start=date_trunc('week',NEW.expense_date)::date) THEN
            RAISE EXCEPTION 'Reopen the driver statement before changing its reimbursement' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
        END IF;
    END IF;
    IF TG_OP='UPDATE' AND old_credit IS DISTINCT FROM new_credit
       AND EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id) THEN
        RAISE EXCEPTION 'An expense used in payroll cannot change between a charge and reimbursement' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_reimbursement_guard BEFORE INSERT OR UPDATE OR DELETE ON expenses
FOR EACH ROW EXECUTE FUNCTION protect_expense_reimbursement();

CREATE INDEX expenses_reimburse_driver_date_idx ON expenses(driver_id,expense_date)
WHERE lower(btrim(payment_type))='reimburse';

COMMIT;
