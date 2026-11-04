BEGIN;
-- Transport/checkpoint metadata only. Canonical loads retain all financial
-- capture and version guards; polling and replay must not invalidate payroll.
CREATE TABLE datatruck_load_versions (
    load_id integer PRIMARY KEY REFERENCES loads(id) ON DELETE CASCADE,
    modified_datetime timestamptz NOT NULL
);
CREATE TABLE datatruck_load_sync_state (
    scope text PRIMARY KEY,
    through_at timestamptz NOT NULL,
    completed_at timestamptz
);
INSERT INTO payroll_projection_sources(table_name,classification)
VALUES ('datatruck_load_versions','unrelated'),('datatruck_load_sync_state','unrelated');
INSERT INTO payroll_cache_revisions(table_name,revision)
VALUES ('datatruck_load_versions',0),('datatruck_load_sync_state',0);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE datatruck_load_versions OWNER TO mserp_app;
  ALTER TABLE datatruck_load_sync_state OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
