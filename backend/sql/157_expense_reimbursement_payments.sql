BEGIN;

-- Separate payout receipts from expense debt collections. Both use the same
-- weekly window, zero deferral, partial amount and cross-week reservation rules.
CREATE TABLE expense_reimbursement_payments (
 expense_id uuid NOT NULL REFERENCES expenses(id) ON DELETE RESTRICT,
 week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1),
 amount numeric(14,2) NOT NULL CHECK(amount>=0),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(expense_id,week_start)
);

-- Reserve credits that were already frozen by the original automatic-credit
-- implementation. Never rewrite those statement snapshots or pay them again.
INSERT INTO expense_reimbursement_payments(expense_id,week_start,amount,updated_by)
SELECT e.id,s.week_start,sum((c->>'amount')::numeric),s.finalized_by
FROM payroll_settlements s CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.report->'autoCharges','[]')) c
JOIN expenses e ON c->>'source'='expense-reimburse:'||e.id::text
WHERE (c->>'amount')::numeric>0
GROUP BY e.id,s.week_start,s.finalized_by;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM expense_reimbursement_payments p JOIN expenses e ON e.id=p.expense_id GROUP BY e.id,e.amount HAVING sum(p.amount)>e.amount) THEN
  RAISE EXCEPTION 'Review duplicate frozen reimbursements before installing the payout ledger';
 END IF;
END $$;

CREATE FUNCTION protect_reimbursement_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE item expenses%ROWTYPE; target uuid; wk date; used numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 IF TG_OP='DELETE' THEN target:=OLD.expense_id;wk:=OLD.week_start;
 ELSE target:=NEW.expense_id;wk:=NEW.week_start; END IF;
 IF TG_OP='UPDATE' AND (NEW.expense_id,NEW.week_start) IS DISTINCT FROM (OLD.expense_id,OLD.week_start) THEN
  RAISE EXCEPTION 'A reimbursement receipt cannot change its expense or week' USING ERRCODE='23514';
 END IF;
 SELECT * INTO item FROM expenses WHERE id=target FOR UPDATE;
 IF lower(btrim(coalesce(item.payment_type,'')))<>'reimburse' OR item.driver_id IS NULL THEN
  RAISE EXCEPTION 'Select a linked Reimburse expense' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=payroll_driver_at(item.driver_id,wk) AND week_start=wk AND finalized) THEN
  RAISE EXCEPTION 'Reopen the driver statement before changing its reimbursement' USING ERRCODE='23514';
 END IF;
 IF TG_OP<>'DELETE' THEN
  IF item.expense_date>=wk+10 THEN RAISE EXCEPTION 'The reimbursement is outside this statement expense window' USING ERRCODE='23514'; END IF;
  SELECT coalesce(sum(amount),0) INTO used FROM expense_reimbursement_payments WHERE expense_id=target AND week_start<>wk;
  IF NEW.amount>item.amount-used THEN RAISE EXCEPTION 'Reimbursement exceeds its available balance' USING ERRCODE='23514'; END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reimbursement_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_reimbursement_payments
FOR EACH ROW EXECUTE FUNCTION protect_reimbursement_payment();
CREATE FUNCTION touch_reimbursement_expense() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE expenses SET updated_at=now() WHERE id=CASE WHEN TG_OP='DELETE' THEN OLD.expense_id ELSE NEW.expense_id END;
 RETURN NULL;
END $$;
CREATE TRIGGER reimbursement_payment_touch AFTER INSERT OR UPDATE OR DELETE ON expense_reimbursement_payments
FOR EACH ROW EXECUTE FUNCTION touch_reimbursement_expense();

CREATE OR REPLACE FUNCTION protect_expense_reimbursement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_credit boolean:=false; new_credit boolean:=false;
BEGIN
 IF TG_OP<>'INSERT' THEN old_credit:=lower(btrim(coalesce(OLD.payment_type,'')))='reimburse'; END IF;
 IF TG_OP<>'DELETE' THEN new_credit:=lower(btrim(coalesce(NEW.payment_type,'')))='reimburse'; END IF;
 IF NOT old_credit AND NOT new_credit THEN IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND (NEW.driver_id,NEW.expense_date,NEW.amount,NEW.payment_type,NEW.expense_type,NEW.category)
 IS NOT DISTINCT FROM (OLD.driver_id,OLD.expense_date,OLD.amount,OLD.payment_type,OLD.expense_type,OLD.category) THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(736281940);
 IF TG_OP<>'INSERT' AND old_credit THEN
  IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized
    AND s.driver_id=payroll_driver_at(OLD.driver_id,s.week_start)
    AND (s.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','expense-reimburse:'||OLD.id::text)))
      OR s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',OLD.id::text,'source','reimburse')))))) THEN
   RAISE EXCEPTION 'Reopen the driver statement before changing its reimbursement' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
  END IF;
  IF TG_OP='UPDATE' AND EXISTS(SELECT 1 FROM expense_reimbursement_payments WHERE expense_id=OLD.id)
   AND (NEW.driver_id,NEW.expense_date,NEW.amount,lower(btrim(NEW.payment_type))) IS DISTINCT FROM (OLD.driver_id,OLD.expense_date,OLD.amount,lower(btrim(OLD.payment_type))) THEN
   RAISE EXCEPTION 'An expense used in payroll cannot change its driver, date, total or reimbursement direction' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
  END IF;
 END IF;
 IF TG_OP<>'DELETE' AND new_credit AND (NEW.driver_id IS NULL OR NEW.amount IS NULL OR NEW.amount<0) THEN
  RAISE EXCEPTION 'Reimburse requires a linked driver and a nonnegative amount' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
 END IF;
 IF TG_OP='UPDATE' AND old_credit IS DISTINCT FROM new_credit AND EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id) THEN
  RAISE EXCEPTION 'An expense used in payroll cannot change between a charge and reimbursement' USING ERRCODE='23514',CONSTRAINT='expense_reimbursement';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN ALTER TABLE expense_reimbursement_payments OWNER TO mserp_app; END IF;
 IF to_regclass('payroll_cache_revisions') IS NOT NULL THEN
  INSERT INTO payroll_cache_revisions(table_name) VALUES('expense_reimbursement_payments');
  CREATE TRIGGER payroll_cache_insert AFTER INSERT ON expense_reimbursement_payments REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
  CREATE TRIGGER payroll_cache_update AFTER UPDATE ON expense_reimbursement_payments REFERENCING OLD TABLE AS old_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
  CREATE TRIGGER payroll_cache_delete AFTER DELETE ON expense_reimbursement_payments REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
  CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON expense_reimbursement_payments FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
 END IF;
END $$;
COMMIT;
