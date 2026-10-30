BEGIN;

-- Retired palette tables still exist after 172. Unregistered tables deliberately
-- force MVCC fallback, so even these unused tables disabled compact payroll reads.
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['status_color_settings','status_color_history'] LOOP
  INSERT INTO payroll_cache_revisions(table_name) VALUES(t) ON CONFLICT DO NOTHING;
  EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 END LOOP;
END $$;
COMMIT;
