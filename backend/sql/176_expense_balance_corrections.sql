BEGIN;
-- Manual corrections change collectible debt, never the invoice or paid ledger.
CREATE TABLE expense_balance_corrections (
 id uuid PRIMARY KEY, expense_id uuid NOT NULL REFERENCES expenses(id),
 remaining numeric(14,2) NOT NULL CHECK(remaining>=0), previous numeric(14,2) NOT NULL CHECK(previous>=0),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 2000), version integer NOT NULL CHECK(version>0),
 actor_id uuid NOT NULL REFERENCES app_users(id), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX expense_balance_corrections_source ON expense_balance_corrections(expense_id);
CREATE FUNCTION expense_balance_adjustment(i uuid) RETURNS numeric LANGUAGE sql STABLE AS $$
 SELECT coalesce(sum(remaining-previous),0) FROM expense_balance_corrections WHERE expense_id=i
$$;
CREATE FUNCTION expense_collectible_total(e expenses) RETURNS numeric LANGUAGE sql STABLE AS $$
 SELECT (CASE WHEN lower(btrim(coalesce(e.payment_type,'')))='reimburse' THEN e.amount WHEN e.driver_settled THEN 0
 WHEN e.base_amount IS NULL AND e.payment_fee=0 AND upper(btrim(e.payment_type))='EFS'
 AND lower(btrim(e.covered_by)) IN ('driver','truck owner') AND e.amount>0
 AND NOT EXISTS(SELECT 1 FROM expense_settings WHERE id=e.category_id AND driver_only)
 THEN e.amount+ceil(e.amount/500)*3 ELSE e.amount END)+expense_balance_adjustment(e.id)
$$;
CREATE TRIGGER expense_balance_immutable BEFORE UPDATE OR DELETE ON expense_balance_corrections
 FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();
CREATE FUNCTION protect_expense_balance_correction() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; frozen boolean;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 SELECT * INTO e FROM expenses WHERE id=NEW.expense_id FOR UPDATE;
 IF NOT FOUND OR e.amount IS NULL OR e.amount<0 OR e.expense_date IS NULL
 OR coalesce(e.charge_driver_id,e.owner_id) IS NULL OR lower(btrim(coalesce(e.payment_type,'')))='reimburse'
 OR NOT accounting_live_source('expense',e.id,e.expense_date) THEN
 RAISE EXCEPTION 'Review the charge amount, date and responsible person before editing its balance' USING ERRCODE='23514'; END IF;
 IF NEW.version<>e.balance_version THEN RAISE EXCEPTION 'The balance changed; reload and review' USING ERRCODE='23514'; END IF;
SELECT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND (
 s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',e.id::text))))
 OR s.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','expense-reimburse:'||e.id::text)))
 OR EXISTS(SELECT 1 FROM expense_payments p JOIN expenses debt ON debt.id=p.expense_id
   WHERE p.expense_id=NEW.expense_id AND p.investor_truck_id IS NULL AND p.week_start=s.week_start
   AND s.driver_id=payroll_driver_at(debt.charge_driver_id,p.week_start))
 OR EXISTS(SELECT 1 FROM expense_reimbursement_payments p JOIN expenses debt ON debt.id=p.expense_id
   WHERE p.expense_id=NEW.expense_id AND p.week_start=s.week_start AND s.driver_id=payroll_driver_at(debt.driver_id,p.week_start))))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks s WHERE s.finalized AND (
 s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',e.id::text))))
 OR EXISTS(SELECT 1 FROM expense_payments p WHERE p.expense_id=e.id AND p.week_start=s.week_start AND p.investor_truck_id=s.truck_id))) INTO frozen;
 IF frozen THEN RAISE EXCEPTION 'Reopen affected finalized statements before editing this balance' USING ERRCODE='23514'; END IF;
 NEW.previous:=expense_collectible_total(e)-expense_external_total(e.id)-coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0);
 IF NEW.previous=NEW.remaining THEN RAISE EXCEPTION 'Enter a different remaining balance' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_balance_guard BEFORE INSERT ON expense_balance_corrections FOR EACH ROW EXECUTE FUNCTION protect_expense_balance_correction();
CREATE TRIGGER expense_balance_touch AFTER INSERT ON expense_balance_corrections FOR EACH ROW EXECUTE FUNCTION touch_external_expense();

CREATE OR REPLACE FUNCTION protect_external_settlement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; original expense_external_settlements%ROWTYPE; used numeric; total numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 SELECT * INTO e FROM expenses WHERE id=NEW.expense_id FOR UPDATE;
 IF NOT accounting_live_source('expense',e.id,e.expense_date) OR (e.driver_settled AND expense_balance_adjustment(e.id)=0) THEN
  RAISE EXCEPTION 'This expense is managed outside active Clustie payroll' USING ERRCODE='23514'; END IF;
 IF e.amount IS NULL OR (CASE WHEN lower(btrim(e.payment_type))='reimburse' THEN e.driver_id ELSE coalesce(e.charge_driver_id,e.owner_id) END) IS NULL THEN
  RAISE EXCEPTION 'Review the amount and responsible person before recording a settlement' USING ERRCODE='23514'; END IF;
 IF NEW.settled_on>(now() AT TIME ZONE 'America/New_York')::date OR NOT accounting_live_date(NEW.settled_on) THEN
  RAISE EXCEPTION 'Use an actual settlement date within active Clustie accounting' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND
 (s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',e.id::text))))
 OR s.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','expense-reimburse:'||e.id::text)))))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks s WHERE s.finalized AND s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',e.id::text))))) THEN
  RAISE EXCEPTION 'Reopen affected finalized statements before recording or reversing an external settlement' USING ERRCODE='23514'; END IF;
 IF NEW.reverses_id IS NOT NULL THEN
  SELECT * INTO original FROM expense_external_settlements WHERE id=NEW.reverses_id;
  IF NOT FOUND OR original.reverses_id IS NOT NULL OR (original.expense_id,original.amount) IS DISTINCT FROM (NEW.expense_id,NEW.amount) THEN
   RAISE EXCEPTION 'Reverse the exact original settlement' USING ERRCODE='23514'; END IF;
 ELSE
  SELECT coalesce(sum(amount),0) INTO used FROM (
   SELECT amount FROM expense_payments WHERE expense_id=e.id UNION ALL
   SELECT amount FROM expense_reimbursement_payments WHERE expense_id=e.id) p;
  total:=expense_collectible_total(e);
  IF NEW.amount>total-used-expense_external_total(e.id) THEN RAISE EXCEPTION 'Settlement exceeds the unreserved balance' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION protect_active_expense_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; used numeric; total numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 SELECT * INTO e FROM expenses WHERE id=NEW.expense_id FOR UPDATE;
 IF NOT accounting_live_source('expense',e.id,e.expense_date) THEN RAISE EXCEPTION 'This expense is managed in Sheets' USING ERRCODE='23514'; END IF;
 SELECT coalesce(sum(amount),0) INTO used FROM (
 SELECT amount FROM expense_payments WHERE expense_id=e.id AND (TG_TABLE_NAME<>'expense_payments' OR week_start<>NEW.week_start)
 UNION ALL SELECT amount FROM expense_reimbursement_payments WHERE expense_id=e.id AND (TG_TABLE_NAME<>'expense_reimbursement_payments' OR week_start<>NEW.week_start)) p;
 total:=expense_collectible_total(e);
 IF NEW.amount>total-used-expense_external_total(e.id) THEN RAISE EXCEPTION 'Payment exceeds the unreserved balance' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION protect_external_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (EXISTS(SELECT 1 FROM expense_external_settlements WHERE expense_id=OLD.id) OR EXISTS(SELECT 1 FROM expense_balance_corrections WHERE expense_id=OLD.id)) AND
 (NEW.driver_id,NEW.charge_driver_id,NEW.owner_id,NEW.truck_id,NEW.expense_date,NEW.amount,NEW.base_amount,NEW.payment_fee,NEW.payment_type,NEW.covered_by,NEW.category_id,NEW.driver_settled)
 IS DISTINCT FROM (OLD.driver_id,OLD.charge_driver_id,OLD.owner_id,OLD.truck_id,OLD.expense_date,OLD.amount,OLD.base_amount,OLD.payment_fee,OLD.payment_type,OLD.covered_by,OLD.category_id,OLD.driver_settled) THEN
 RAISE EXCEPTION 'A settled or balance-corrected expense retains its identity and principal' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;

DO $$ DECLARE t text:='expense_balance_corrections'; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN ALTER TABLE expense_balance_corrections OWNER TO mserp_app; END IF;
 INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
 EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
END $$;
COMMIT;
