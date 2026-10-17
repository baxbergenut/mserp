BEGIN;

-- Poll evidence is distinct from human edits/history and never changes progress.
CREATE TABLE driver_board_automation (
 driver_id uuid PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
 evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
 travel jsonb NOT NULL DEFAULT '{}'::jsonb,
 eta_context text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON driver_board_automation
FOR EACH ROW EXECUTE FUNCTION notify_status_board_change();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_board_automation OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;
