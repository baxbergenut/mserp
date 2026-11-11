BEGIN;

CREATE TABLE gross_board_week_locks (
    week_start date PRIMARY KEY CHECK (extract(isodow FROM week_start) = 1),
    locked boolean NOT NULL,
    version integer NOT NULL CHECK (version > 0)
);

INSERT INTO payroll_projection_sources(table_name,classification) VALUES ('gross_board_week_locks','unrelated');
INSERT INTO payroll_cache_revisions(table_name,revision) VALUES ('gross_board_week_locks',0);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
  GRANT SELECT, INSERT, UPDATE ON gross_board_week_locks TO mserp_app;
 END IF;
END $$;

COMMIT;
