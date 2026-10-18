BEGIN;

-- A shared order per dispatcher; the empty key represents unassigned drivers.
CREATE TABLE driver_board_driver_order (
 dispatcher_key text PRIMARY KEY,
 dispatcher_id uuid REFERENCES dispatchers(id) ON DELETE CASCADE,
 driver_ids uuid[] NOT NULL,
 version bigint NOT NULL DEFAULT 1,
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (dispatcher_key = coalesce(dispatcher_id::text,''))
);
CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON driver_board_driver_order
FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change();
INSERT INTO payroll_cache_revisions(table_name) VALUES('driver_board_driver_order');
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_board_driver_order OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;
