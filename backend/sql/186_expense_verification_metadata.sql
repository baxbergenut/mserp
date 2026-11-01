-- Verification changes no payroll amount, attribution, or collection revision.
BEGIN;
CREATE FUNCTION payroll_expense_verification_change(prior jsonb, following jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT prior->'accounting_verified' IS DISTINCT FROM following->'accounting_verified'
 AND (prior-ARRAY['accounting_verified','updated_at']) IS NOT DISTINCT FROM
     (following-ARRAY['accounting_verified','updated_at'])
$$;

-- Keep the financial version stable for this exact metadata transition. Every
-- other update, including collection touches and mixed edits, keeps its guard.
DO $$ DECLARE definition text; BEGIN
 definition:=replace(pg_get_functiondef('protect_expense_balance()'::regprocedure),E'\r\n',E'\n');
 IF strpos(definition,'NEW.balance_version := OLD.balance_version + 1;')=0 THEN
  RAISE EXCEPTION 'Unexpected expense balance guard';
 END IF;
 definition:=replace(definition,'NEW.balance_version := OLD.balance_version + 1;',
  'IF payroll_expense_verification_change(to_jsonb(OLD),to_jsonb(NEW)) THEN RETURN NEW; END IF;
        NEW.balance_version := OLD.balance_version + 1;');
 EXECUTE definition;
END $$;

-- This transaction mode is narrowly validated for EVERY changed row, after
-- the other BEFORE-row guards. It cannot authorize a financial edit or delete.
DO $$ DECLARE definition text; BEGIN
 definition:=replace(pg_get_functiondef('guard_payroll_protocol_statement()'::regprocedure),E'\r\n',E'\n');
 definition:=replace(definition,E'BEGIN\n',E'BEGIN\n IF TG_TABLE_NAME=''expenses'' AND TG_OP=''UPDATE'' AND current_setting(''mserp.payroll.expense_verification'',true)=''on'' THEN RETURN NULL; END IF;\n');
 EXECUTE definition;
 definition:=replace(pg_get_functiondef('guard_payroll_protocol_row()'::regprocedure),E'\r\n',E'\n');
 definition:=replace(definition,E'BEGIN\n',E'BEGIN\n IF TG_TABLE_NAME=''expenses'' AND current_setting(''mserp.payroll.expense_verification'',true)=''on'' THEN\n  IF TG_OP<>''UPDATE'' OR NOT payroll_expense_verification_change(to_jsonb(OLD),to_jsonb(NEW)) THEN\n   RAISE EXCEPTION ''Verification cannot change expense financial data'' USING ERRCODE=''23514'';\n  END IF;\n  RETURN NEW;\n END IF;\n');
 EXECUTE definition;
 definition:=replace(pg_get_functiondef('capture_payroll_projection_change()'::regprocedure),E'\r\n',E'\n');
 definition:=replace(definition,E'BEGIN\n',E'BEGIN\n IF TG_TABLE_NAME=''expenses'' AND TG_OP=''UPDATE'' THEN\n  IF NOT EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id) WHERE NOT payroll_expense_verification_change(to_jsonb(o),to_jsonb(n))) THEN RETURN NULL; END IF;\n END IF;\n');
 EXECUTE definition;
END $$;

CREATE FUNCTION invalidate_payroll_expense_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
  WHERE NOT payroll_expense_verification_change(to_jsonb(o),to_jsonb(n))) THEN RETURN NULL; END IF;
 IF payroll_resource_command() THEN RETURN NULL; END IF;
 UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='expenses';
 RETURN NULL;
END $$;
DROP TRIGGER payroll_cache_update ON expenses;
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON expenses
 REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_expense_cache();
COMMIT;
