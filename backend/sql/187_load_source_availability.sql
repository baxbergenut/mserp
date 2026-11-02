BEGIN;

-- Operational provider-presence evidence only. Never remove or change historical
-- load, Gross Board or payroll source records when a filtered lookup omits an ID.
CREATE TABLE load_source_availability (
    load_id integer PRIMARY KEY REFERENCES loads(id) ON DELETE CASCADE,
    present boolean NOT NULL,
    checked_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO payroll_projection_sources(table_name,classification)
VALUES ('load_source_availability','unrelated');
INSERT INTO payroll_cache_revisions(table_name,revision)
VALUES ('load_source_availability',0);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE load_source_availability OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;
