BEGIN;
-- Explicit, dispatch-only text. Existing ambiguous/manual loads and canonical
-- statuses are deliberately left untouched. Tombstones retain optimistic versions.
CREATE TABLE gross_board_planning_entries (
    driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    service_date date NOT NULL CHECK (service_date BETWEEN '2000-01-03' AND '2101-01-02'),
    kind text NOT NULL CHECK (kind IN ('note','destination')),
    text text NOT NULL DEFAULT '' CHECK (char_length(text)<=1000),
    version integer NOT NULL DEFAULT 0 CHECK (version>=0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
    PRIMARY KEY(driver_id,service_date,kind)
);
CREATE INDEX gross_board_planning_entries_week ON gross_board_planning_entries(service_date);
-- Row-level delivery avoids invalidating snapshots for ON CONFLICT DO NOTHING
-- retries. PostgreSQL delivers the existing board notification only on commit.
CREATE TRIGGER gross_board_planning_inserted AFTER INSERT ON gross_board_planning_entries
FOR EACH ROW WHEN (NEW.text<>'') EXECUTE FUNCTION notify_gross_board_change();
CREATE TRIGGER gross_board_planning_updated AFTER UPDATE ON gross_board_planning_entries
FOR EACH ROW WHEN (OLD.text IS DISTINCT FROM NEW.text) EXECUTE FUNCTION notify_gross_board_change();
CREATE TRIGGER gross_board_planning_deleted AFTER DELETE ON gross_board_planning_entries
FOR EACH ROW WHEN (OLD.text<>'') EXECUTE FUNCTION notify_gross_board_change();
-- No load/queue/provider/export writer or financial source reads this table.
INSERT INTO payroll_projection_sources(table_name,classification) VALUES ('gross_board_planning_entries','unrelated');
INSERT INTO payroll_cache_revisions(table_name,revision) VALUES ('gross_board_planning_entries',0);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE gross_board_planning_entries OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
