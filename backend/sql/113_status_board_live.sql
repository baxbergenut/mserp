BEGIN;

-- Invalidations are delivered only after commit and never contain board data.
CREATE FUNCTION notify_status_board_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('mserp_driver_board','');
    RETURN NULL;
END $$;
DO $$ DECLARE name text; BEGIN
    FOREACH name IN ARRAY ARRAY['driver_board','driver_board_load_state','drivers','trucks','investors',
      'dispatchers','updaters','dispatcher_updaters','truck_driver_assignments','driver_teams',
      'gross_board_entries','gross_board_extra_entries','loads','five_eld_locations'] LOOP
        EXECUTE format('CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change()',name);
    END LOOP;
END $$;

-- Tab-scoped, expiring cursors. Session deletion removes presence immediately.
CREATE TABLE driver_board_presence (
    client_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    session_hash char(64) NOT NULL REFERENCES auth_sessions(token_hash) ON DELETE CASCADE,
    driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    column_key text NOT NULL CHECK(column_key IN ('currentLoad','driver','truck','trailerNumber','originalGross','driverGross','phone','status','destination','notes','eta','loadPlan','homeTime')),
    expires_at timestamptz NOT NULL
);
CREATE INDEX driver_board_presence_expiry ON driver_board_presence(expires_at);
CREATE FUNCTION notify_board_presence_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.driver_id=NEW.driver_id AND OLD.column_key=NEW.column_key THEN RETURN NULL; END IF;
    PERFORM pg_notify('mserp_board_presence','');
    RETURN NULL;
END $$;
CREATE TRIGGER board_presence_changed AFTER INSERT OR UPDATE OR DELETE ON driver_board_presence
FOR EACH ROW EXECUTE FUNCTION notify_board_presence_change();
INSERT INTO payroll_cache_revisions(table_name) VALUES('driver_board_presence');
-- Presence is operational metadata and is excluded by payroll_cache.go.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board_presence OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
