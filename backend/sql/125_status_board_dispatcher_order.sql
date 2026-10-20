BEGIN;

-- One shared dispatcher-group order, including the empty Unassigned key.
CREATE TABLE driver_board_dispatcher_order (
 id boolean PRIMARY KEY DEFAULT true CHECK (id),
 dispatcher_ids text[] NOT NULL,
 version bigint NOT NULL DEFAULT 1,
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON driver_board_dispatcher_order
FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change();
INSERT INTO payroll_cache_revisions(table_name) VALUES('driver_board_dispatcher_order');
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_board_dispatcher_order OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;
