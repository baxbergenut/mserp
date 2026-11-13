BEGIN;
CREATE TABLE board_stop_geocodes (
    coordinate text PRIMARY KEY,
    next_attempt_at timestamptz NOT NULL,
    address jsonb
);
INSERT INTO payroll_projection_sources(table_name,classification) VALUES ('board_stop_geocodes','unrelated');
INSERT INTO payroll_cache_revisions(table_name,revision) VALUES ('board_stop_geocodes',0);
CREATE TRIGGER status_board_stop_city_changed AFTER UPDATE ON board_stop_geocodes
FOR EACH ROW WHEN (OLD.address IS DISTINCT FROM NEW.address) EXECUTE FUNCTION notify_status_board_change();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE board_stop_geocodes OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
