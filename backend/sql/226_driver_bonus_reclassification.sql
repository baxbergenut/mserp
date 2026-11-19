-- A linked owner expense may have an open driver payout reservation. Selecting
-- Bonuses keeps that same payout, week, driver, amount and owner obligation.
DO $migration$
DECLARE
 guard regprocedure := to_regprocedure(format('%I.guard_expense_accessorial()',current_schema()));
 definition text;
 old_clause text := 'OR EXISTS(SELECT 1 FROM expense_reimbursement_payments WHERE expense_id=OLD.id)';
 new_clause text := $clause$OR (EXISTS(SELECT 1 FROM expense_reimbursement_payments WHERE expense_id=OLD.id)
        AND NOT (OLD.owner_recovery
          AND lower(btrim(coalesce(OLD.payment_type,'')))='reimburse'
          AND EXISTS(SELECT 1 FROM expense_settings WHERE id=NEW.category_id AND kind='category' AND active AND lower(btrim(name))='bonuses')
          AND NEW.category_id IS DISTINCT FROM OLD.category_id
          AND (NEW.driver_id,NEW.truck_id,NEW.owner_id,NEW.covered_by,NEW.amount,NEW.expense_date,NEW.payment_type)
            IS NOT DISTINCT FROM (OLD.driver_id,OLD.truck_id,OLD.owner_id,OLD.covered_by,OLD.amount,OLD.expense_date,OLD.payment_type)
          AND NOT EXISTS(SELECT 1 FROM expense_reimbursement_payments p JOIN payroll_settlements s
            ON s.driver_id=payroll_driver_at(OLD.driver_id,p.week_start) AND s.week_start=p.week_start
            WHERE p.expense_id=OLD.id AND s.finalized)))$clause$;
BEGIN
 -- Earlier standalone fixture schemas can predate the accessorial guard.
 IF guard IS NULL THEN RETURN; END IF;
 definition := pg_get_functiondef(guard);
 IF strpos(definition,old_clause)=0 THEN RAISE EXCEPTION 'Unexpected owner expense payout guard'; END IF;
 EXECUTE replace(definition,old_clause,new_clause);
END $migration$;
