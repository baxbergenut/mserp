BEGIN;
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup'));
INSERT INTO system_task_assignments(kind,assignee_id,assignee_ids)
 SELECT v.kind,a.assignee_id,a.assignee_ids FROM system_task_assignments a CROSS JOIN (VALUES('relay_setup'),('weighmytruck_setup')) v(kind) WHERE a.kind='driver_onboarding';
ALTER TABLE system_task_records ADD COLUMN available_on date NOT NULL DEFAULT (now() AT TIME ZONE 'America/New_York')::date;
ALTER TABLE system_task_records ADD COLUMN service_driver_id uuid;
CREATE TABLE driver_service_requests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
 service text NOT NULL CHECK(service IN ('relay','weighmytruck')),
 state text NOT NULL CHECK(state IN ('queued','running','review','deferred','done','canceled')),
 version integer NOT NULL DEFAULT 1,
 follow_up_on date NOT NULL DEFAULT (now() AT TIME ZONE 'America/New_York')::date,
 reminder_notified_on date,
 last_error text NOT NULL DEFAULT '',
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(driver_id,service)
);
CREATE INDEX driver_service_requests_work_idx ON driver_service_requests(state,created_at);
CREATE TABLE driver_service_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 request_id uuid NOT NULL,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 state text NOT NULL,
 follow_up_on date NOT NULL,
 detail text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION sync_driver_service_task() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver_name text; label text;
BEGIN
 IF TG_OP='DELETE' THEN
  UPDATE system_task_records SET completed_at=coalesce(completed_at,now()),completed_by_name='System',outcome='Driver deleted' WHERE id=OLD.id;
  RETURN OLD;
 END IF;
 SELECT full_name INTO driver_name FROM drivers WHERE id=NEW.driver_id;
 label=CASE NEW.service WHEN 'relay' THEN 'Relay' ELSE 'WeighMyTruck' END;
 INSERT INTO system_task_records(id,kind,title,notes,created_at,available_on,service_driver_id,completed_at,completed_by_name,outcome)
 VALUES(NEW.id,NEW.service||'_setup','Set up '||driver_name||' in '||label,
 concat_ws(E'\n',nullif(NEW.last_error,''),'Follow up: '||NEW.follow_up_on::text),NEW.created_at,NEW.follow_up_on,NEW.driver_id,
 CASE WHEN NEW.state IN ('done','canceled') THEN now() END,CASE WHEN NEW.state IN ('done','canceled') THEN 'System' ELSE '' END,
 CASE NEW.state WHEN 'done' THEN 'Enrollment confirmed' WHEN 'canceled' THEN 'Driver terminated; setup canceled' ELSE '' END)
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,notes=excluded.notes,available_on=excluded.available_on,
 completed_at=CASE WHEN NEW.state IN ('done','canceled') THEN coalesce(system_task_records.completed_at,excluded.completed_at) ELSE NULL END,
 completed_by_name=excluded.completed_by_name,outcome=excluded.outcome;
 INSERT INTO driver_service_events(request_id,actor_id,state,follow_up_on,detail) VALUES(NEW.id,NEW.actor_id,NEW.state,NEW.follow_up_on,NEW.last_error);
 RETURN NEW;
END $$;
CREATE TRIGGER driver_service_task AFTER INSERT OR UPDATE OR DELETE ON driver_service_requests FOR EACH ROW EXECUTE FUNCTION sync_driver_service_task();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_service_requests OWNER TO mserp_app;
  ALTER TABLE driver_service_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
