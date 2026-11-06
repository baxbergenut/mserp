BEGIN;
SELECT pg_advisory_xact_lock(736281940);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM expenses WHERE payment_type='Driver payroll') THEN
  RAISE EXCEPTION 'Existing Driver payroll expenses require review before reserving the accessorial payment method';
 END IF;
END $$;

ALTER TABLE expenses ADD COLUMN owner_recovery boolean NOT NULL DEFAULT false,
 ADD COLUMN accessorial_request_id uuid UNIQUE, ADD COLUMN accessorial_request_hash text;
ALTER TABLE expenses ADD CONSTRAINT accessorial_request_pair CHECK ((accessorial_request_id IS NULL)=(accessorial_request_hash IS NULL));

INSERT INTO expense_settings(id,kind,name,active)
VALUES('00000000-0000-0000-0000-000000000203','category','Accessorials',true)
ON CONFLICT DO NOTHING;
-- An existing category of this name can be used without reclassifying its invoices.
INSERT INTO expense_settings(kind,category_id,name)
SELECT 'name',s.id,t.name FROM expense_settings s CROSS JOIN
 (VALUES ('Detention'),('Layover'),('Extra stop'),('DOT inspection bonus'),('Other')) t(name)
WHERE s.kind='category' AND lower(s.name)='accessorials' ON CONFLICT DO NOTHING;
INSERT INTO role_expense_category_access(role_id,category_id,can_view,can_create,can_edit,can_delete)
SELECT r.id,s.id,true,true,true,true FROM app_roles r CROSS JOIN expense_settings s
WHERE lower(r.name)='accountant' AND s.kind='category' AND lower(s.name)='accessorials'
ON CONFLICT DO NOTHING;
ALTER TABLE expense_settings ADD COLUMN accessorials boolean NOT NULL DEFAULT false;
UPDATE expense_settings SET accessorials=true,active=true WHERE kind='category' AND lower(name)='accessorials';

CREATE TABLE expense_accessorial_payments(
 expense_id uuid NOT NULL REFERENCES expenses(id),
 week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1),
 driver_id uuid NOT NULL REFERENCES drivers(id),
 amount numeric(14,2) NOT NULL CHECK(amount>=0),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(expense_id,week_start)
);
CREATE INDEX expenses_accessorial_driver ON expenses(driver_id) WHERE payment_type='Driver payroll';

CREATE FUNCTION guard_expense_accessorial() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a boolean; old_financial boolean:=false; special boolean;
BEGIN
 IF TG_OP<>'INSERT' THEN
  old_financial:=OLD.payment_type IS NOT DISTINCT FROM 'Driver payroll' OR OLD.owner_recovery;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 IF TG_OP='INSERT' THEN
  NEW.owner_recovery:=coalesce(lower(btrim(NEW.payment_type))='reimburse' AND lower(btrim(NEW.covered_by))='truck owner',false);
 ELSE
  NEW.owner_recovery:=OLD.owner_recovery;
 END IF;
 SELECT accessorials INTO a FROM expense_settings WHERE id=NEW.category_id;
 IF a AND (TG_OP='INSERT' OR NEW.category_id IS DISTINCT FROM OLD.category_id) THEN
  IF coalesce(NEW.payment_type,'Driver payroll')<>'Driver payroll' THEN
   RAISE EXCEPTION 'Select Driver payroll for an accessorial' USING ERRCODE='23514';
  END IF;
  NEW.payment_type:='Driver payroll';
 END IF;
 IF NEW.payment_type='Driver payroll' THEN
  IF NOT coalesce(a,false) OR NEW.driver_id IS NULL OR NEW.applicant_id IS NOT NULL OR NEW.amount IS NULL OR NEW.amount<=0
     OR NEW.expense_date IS NULL OR coalesce(btrim(NEW.expense_type),'')='' OR NEW.payment_fee<>0
     OR coalesce(lower(btrim(NEW.covered_by)),'') NOT IN ('company','truck owner') THEN
   RAISE EXCEPTION 'Accessorials require a driver, expense date, type, positive amount and Company or Truck Owner coverage' USING ERRCODE='23514';
  END IF;
 END IF;
 special:=NEW.payment_type IS NOT DISTINCT FROM 'Driver payroll' OR NEW.owner_recovery;
 IF special AND lower(btrim(NEW.covered_by))='truck owner' AND (
    NEW.truck_id IS NULL OR NEW.owner_id IS NULL OR
    ((TG_OP='INSERT' OR (NEW.truck_id,NEW.owner_id,NEW.covered_by) IS DISTINCT FROM (OLD.truck_id,OLD.owner_id,OLD.covered_by))
      AND NOT EXISTS(SELECT 1 FROM trucks t JOIN investors i ON i.id=t.owner_id
        WHERE t.id=NEW.truck_id AND t.owner_id=NEW.owner_id AND NOT i.is_company))) THEN
  RAISE EXCEPTION 'Select the truck and its confirmed owner' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND (old_financial OR special) AND
    (NEW.driver_id,NEW.truck_id,NEW.owner_id,NEW.covered_by,NEW.amount,NEW.expense_date,NEW.payment_type,NEW.category_id,NEW.expense_type)
    IS DISTINCT FROM (OLD.driver_id,OLD.truck_id,OLD.owner_id,OLD.covered_by,OLD.amount,OLD.expense_date,OLD.payment_type,OLD.category_id,OLD.expense_type)
    AND (EXISTS(SELECT 1 FROM expense_accessorial_payments WHERE expense_id=OLD.id)
      OR EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=OLD.id)
      OR EXISTS(SELECT 1 FROM expense_reimbursement_payments WHERE expense_id=OLD.id)
      OR EXISTS(SELECT 1 FROM expense_external_settlements WHERE expense_id=OLD.id)
      OR EXISTS(SELECT 1 FROM expense_balance_corrections WHERE expense_id=OLD.id)) THEN
  RAISE EXCEPTION 'Reopen affected statements and clear posted payments before changing this expense' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND old_financial AND NEW.payment_type IS DISTINCT FROM OLD.payment_type THEN
  RAISE EXCEPTION 'This expense retains its payment method' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_01_accessorial BEFORE INSERT OR UPDATE ON expenses
 FOR EACH ROW EXECUTE FUNCTION guard_expense_accessorial();

CREATE FUNCTION protect_accessorial_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; p expense_accessorial_payments%ROWTYPE; used numeric;
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'Reverse the exact accessorial posting before replacing it' USING ERRCODE='23514'; END IF;
 IF TG_OP='DELETE' THEN p:=OLD; ELSE p:=NEW; END IF;
 SELECT * INTO e FROM expenses WHERE id=p.expense_id FOR UPDATE;
 IF e.payment_type IS DISTINCT FROM 'Driver payroll' OR NOT accounting_live_source('expense',e.id,e.expense_date)
 OR (TG_OP='INSERT' AND p.driver_id IS DISTINCT FROM payroll_driver_at(e.driver_id,p.week_start))
 OR EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=p.driver_id AND week_start=p.week_start AND finalized) THEN
  RAISE EXCEPTION 'Review or reopen the driver statement before changing this accessorial payout' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 SELECT coalesce(sum(amount),0) INTO used FROM expense_accessorial_payments WHERE expense_id=e.id;
 IF p.amount>e.amount-used THEN RAISE EXCEPTION 'Accessorial payout exceeds its unpaid balance' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER accessorial_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_accessorial_payments
 FOR EACH ROW EXECUTE FUNCTION protect_accessorial_payment();

-- A reimbursement's existing external settlements belong to the payout side.
-- Keep those and the driver payout ledger out of the new owner balance.
CREATE OR REPLACE FUNCTION protect_active_expense_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; used numeric; total numeric; external_paid numeric;
BEGIN
 PERFORM payroll_financial_gate(false);
 SELECT * INTO e FROM expenses WHERE id=NEW.expense_id FOR UPDATE;
 IF NOT accounting_live_source('expense',e.id,e.expense_date) THEN RAISE EXCEPTION 'This expense is managed in Sheets' USING ERRCODE='23514'; END IF;
 IF (e.owner_recovery OR e.payment_type='Driver payroll') AND TG_TABLE_NAME='expense_payments' AND
    (lower(btrim(coalesce(e.covered_by,'')))<>'truck owner' OR e.owner_id IS NULL OR e.truck_id IS NULL) THEN
  RAISE EXCEPTION 'This expense has no owner obligation' USING ERRCODE='23514';
 END IF;
 SELECT coalesce(sum(amount),0) INTO used FROM (
 SELECT amount FROM expense_payments WHERE expense_id=e.id AND (TG_TABLE_NAME<>'expense_payments' OR week_start<>NEW.week_start)
 AND (NOT e.owner_recovery OR TG_TABLE_NAME='expense_payments')
 UNION ALL SELECT amount FROM expense_reimbursement_payments WHERE expense_id=e.id
 AND (TG_TABLE_NAME<>'expense_reimbursement_payments' OR week_start<>NEW.week_start)
 AND (NOT e.owner_recovery OR TG_TABLE_NAME='expense_reimbursement_payments')) p;
 total:=expense_collectible_total(e);
 external_paid:=CASE WHEN e.owner_recovery AND TG_TABLE_NAME='expense_payments' THEN 0 ELSE expense_external_total(e.id) END;
 IF NEW.amount>total-used-external_paid THEN RAISE EXCEPTION 'Payment exceeds the unreserved balance' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION guard_linked_expense_manual_settlement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM expenses WHERE id=NEW.expense_id AND (owner_recovery OR payment_type='Driver payroll')) THEN
  RAISE EXCEPTION 'Settle this expense through its driver or owner payroll statement' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER aa_linked_expense_manual_guard BEFORE INSERT ON expense_external_settlements
 FOR EACH ROW EXECUTE FUNCTION guard_linked_expense_manual_settlement();
CREATE TRIGGER aa_linked_expense_manual_guard BEFORE INSERT ON expense_balance_corrections
 FOR EACH ROW EXECUTE FUNCTION guard_linked_expense_manual_settlement();

DO $$ DECLARE t text:='expense_accessorial_payments'; op text; clause text; definition text; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',t); END IF;
 INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
 INSERT INTO payroll_projection_sources(table_name,classification) VALUES(t,'financial');
 definition:=pg_get_functiondef('payroll_resource_table(text)'::regprocedure);
 EXECUTE replace(definition,'''expense_reimbursement_payments''','''expense_reimbursement_payments'',''expense_accessorial_payments''');
 EXECUTE format('CREATE TRIGGER payroll_protocol_statement BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION guard_payroll_protocol_statement()',t);
 EXECUTE format('CREATE TRIGGER payroll_protocol_row BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_payroll_protocol_row()',t);
 FOREACH op IN ARRAY ARRAY['insert','update','delete','truncate'] LOOP
  clause:=CASE op WHEN 'insert' THEN 'REFERENCING NEW TABLE AS changed_rows'
    WHEN 'update' THEN 'REFERENCING OLD TABLE AS old_rows NEW TABLE AS changed_rows'
    WHEN 'delete' THEN 'REFERENCING OLD TABLE AS changed_rows' ELSE '' END;
  EXECUTE format('CREATE TRIGGER %I AFTER %s ON %I %s FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()','payroll_cache_'||op,upper(op),t,clause);
  IF op='update' THEN clause:=replace(clause,'OLD TABLE AS old_rows','OLD TABLE AS previous_rows'); END IF;
  IF op='delete' THEN clause:='REFERENCING OLD TABLE AS previous_rows'; END IF;
  EXECUTE format('CREATE TRIGGER %I AFTER %s ON %I %s FOR EACH STATEMENT EXECUTE FUNCTION capture_payroll_projection_change()','payroll_projection_'||op,upper(op),t,clause);
 END LOOP;
END $$;
COMMIT;
