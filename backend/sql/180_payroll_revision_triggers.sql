BEGIN;
-- Complete event coverage for already enrolled inputs. Some later migrations
-- only installed the write operations used at the time. Keep unknown future
-- tables unregistered so the conservative runtime fallback still applies.
DO $$ DECLARE t text; op text; clause text; BEGIN
 FOR t IN SELECT p.tablename FROM pg_tables p JOIN payroll_cache_revisions r ON r.table_name=p.tablename
  WHERE p.schemaname=current_schema() ORDER BY p.tablename LOOP
  FOREACH op IN ARRAY ARRAY['insert','update','delete','truncate'] LOOP
   IF NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass(quote_ident(t)) AND tgname='payroll_cache_'||op) THEN
    clause=CASE op WHEN 'insert' THEN 'REFERENCING NEW TABLE AS changed_rows'
     WHEN 'update' THEN 'REFERENCING NEW TABLE AS changed_rows'
     WHEN 'delete' THEN 'REFERENCING OLD TABLE AS changed_rows' ELSE '' END;
    EXECUTE format('CREATE TRIGGER %I AFTER %s ON %I %s FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()','payroll_cache_'||op,upper(op),t,clause);
   END IF;
  END LOOP;
 END LOOP;
END $$;
COMMIT;
