BEGIN;

-- Legacy/imported EFS principal remains unchanged. Payroll computes the missing
-- $3 per started $500 once in the shared collectible balance. Once payments
-- exist, metadata must not silently change that balance under another week.
CREATE FUNCTION protect_expense_settlement_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_total numeric; new_total numeric;
BEGIN
    IF (NEW.amount,NEW.base_amount,NEW.payment_fee,NEW.payment_type,NEW.covered_by,NEW.category_id,NEW.driver_settled)
       IS NOT DISTINCT FROM
       (OLD.amount,OLD.base_amount,OLD.payment_fee,OLD.payment_type,OLD.covered_by,OLD.category_id,OLD.driver_settled) THEN
        RETURN NEW;
    END IF;
    IF NOT OLD.driver_settled AND NOT EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id) THEN
        RETURN NEW;
    END IF;
    SELECT CASE WHEN e.base_amount IS NULL AND e.payment_fee=0
      AND upper(btrim(e.payment_type))='EFS' AND lower(btrim(e.covered_by)) IN ('driver','truck owner')
      AND e.amount>0 AND NOT e.driver_settled
      AND NOT EXISTS(SELECT 1 FROM expense_settings c WHERE c.id=e.category_id AND c.driver_only)
      THEN e.amount+ceil(e.amount/500)*3 ELSE e.amount END
    INTO old_total FROM (SELECT OLD.*) e;
    SELECT CASE WHEN e.base_amount IS NULL AND e.payment_fee=0
      AND upper(btrim(e.payment_type))='EFS' AND lower(btrim(e.covered_by)) IN ('driver','truck owner')
      AND e.amount>0 AND NOT e.driver_settled
      AND NOT EXISTS(SELECT 1 FROM expense_settings c WHERE c.id=e.category_id AND c.driver_only)
      THEN e.amount+ceil(e.amount/500)*3 ELSE e.amount END
    INTO new_total FROM (SELECT NEW.*) e;
    IF old_total IS DISTINCT FROM new_total THEN
        RAISE EXCEPTION 'An expense used in a settlement cannot change its EFS fee or collectible total' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expenses_settlement_total_guard BEFORE UPDATE ON expenses
FOR EACH ROW EXECUTE FUNCTION protect_expense_settlement_total();

COMMIT;
