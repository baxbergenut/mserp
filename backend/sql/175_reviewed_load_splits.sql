-- Reviewed revenue allocations are independent of provider trip pay.
ALTER TABLE loads ADD COLUMN split_allocation jsonb;
ALTER TABLE loads ADD CONSTRAINT split_allocation_object CHECK(split_allocation IS NULL OR jsonb_typeof(split_allocation)='object');

CREATE TABLE load_split_history (
 id bigserial PRIMARY KEY, load_id integer NOT NULL REFERENCES loads(id),
 actor_id text NOT NULL, changed_at timestamptz NOT NULL DEFAULT now(),
 before_values jsonb, after_values jsonb NOT NULL
);
CREATE FUNCTION audit_load_split() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.split_allocation IS DISTINCT FROM OLD.split_allocation THEN
  IF NEW.split_allocation IS NULL THEN
   RAISE EXCEPTION 'A reviewed split cannot be removed' USING ERRCODE='23514';
  END IF;
  INSERT INTO load_split_history(load_id,actor_id,before_values,after_values)
  VALUES(NEW.id,coalesce(current_setting('mserp.board_actor',true),''),OLD.split_allocation,NEW.split_allocation);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER load_split_audit AFTER UPDATE OF split_allocation ON loads FOR EACH ROW EXECUTE FUNCTION audit_load_split();
CREATE TRIGGER load_split_history_immutable BEFORE UPDATE OR DELETE ON load_split_history FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE load_split_history OWNER TO mserp_app;
  ALTER SEQUENCE load_split_history_id_seq OWNER TO mserp_app;
 END IF;
END $$;
INSERT INTO payroll_cache_revisions(table_name) VALUES('load_split_history');
CREATE TRIGGER payroll_cache_insert AFTER INSERT ON load_split_history REFERENCING NEW TABLE AS changed_rows
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON load_split_history
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
