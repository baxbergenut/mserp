BEGIN;
SELECT pg_advisory_xact_lock(736281941);

-- Company purchases with no payroll identity are not statement inputs.
-- Check the actual inserted row after responsibility and applicant routing.
CREATE FUNCTION guard_payroll_company_expense() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('mserp.payroll.company_expense',true)='on' AND (
  lower(btrim(coalesce(NEW.covered_by,'')))<>'company'
  OR NEW.driver_id IS NOT NULL OR NEW.charge_driver_id IS NOT NULL
  OR NEW.truck_id IS NOT NULL OR NEW.owner_id IS NOT NULL OR NEW.applicant_id IS NOT NULL
  OR NEW.system_kind IS NOT NULL OR nullif(btrim(NEW.broker_load_number),'') IS NOT NULL
  OR lower(btrim(coalesce(NEW.payment_type,'')))='reimburse'
  OR EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=NEW.category_id)
 ) THEN
  RAISE EXCEPTION 'Expense requires normal payroll validation'
   USING ERRCODE='23514',CONSTRAINT='payroll_company_expense';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER zz_payroll_company_expense BEFORE INSERT ON expenses
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_company_expense();

DO $$ DECLARE definition text; prefix text; BEGIN
 prefix:=$prefix$BEGIN
 IF current_setting('mserp.payroll.company_expense',true)='on' THEN
  IF TG_TABLE_NAME='expenses' AND TG_OP='INSERT' AND NOT payroll_resource_command() THEN RETURN NULL; END IF;
  RAISE EXCEPTION 'Company expense insert cannot change payroll sources' USING ERRCODE='23514';
 END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('guard_payroll_protocol_statement()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',prefix);
 prefix:=$prefix$BEGIN
 IF TG_TABLE_NAME='expenses' AND TG_OP='INSERT' AND current_setting('mserp.payroll.company_expense',true)='on' THEN RETURN NULL; END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('capture_payroll_projection_change()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',prefix);
 definition:=replace(pg_get_functiondef('invalidate_payroll_cache()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',prefix);
END $$;
COMMIT;
