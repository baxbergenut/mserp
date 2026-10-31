BEGIN;
-- Synchronization bookkeeping is not a payroll input. Compare every other
-- column, including the complete source payload and future added columns.
CREATE FUNCTION invalidate_payroll_load_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
  WHERE (to_jsonb(o)-'synced_at') IS DISTINCT FROM (to_jsonb(n)-'synced_at')
 ) THEN
  UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='loads';
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER payroll_cache_update ON loads;
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON loads
 REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_load_cache();
COMMIT;
