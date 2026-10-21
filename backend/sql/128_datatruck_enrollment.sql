BEGIN;
ALTER TABLE driver_service_requests DROP CONSTRAINT driver_service_requests_service_check;
ALTER TABLE driver_service_requests ADD CONSTRAINT driver_service_requests_service_check CHECK(service IN ('relay','weighmytruck','datatruck'));
ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','datatruck_setup','five_eld_truck','maintenance_pm'));
INSERT INTO system_task_assignments(kind,assignee_id,assignee_ids)
 SELECT 'datatruck_setup',assignee_id,assignee_ids FROM system_task_assignments WHERE kind='driver_onboarding';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='system_task_assignments'::regclass AND attname='assignee_role_ids' AND NOT attisdropped) THEN
  EXECUTE 'UPDATE system_task_assignments d SET assignee_role_ids=s.assignee_role_ids FROM system_task_assignments s WHERE d.kind=''datatruck_setup'' AND s.kind=''driver_onboarding''';
 END IF;
END $$;
CREATE TABLE datatruck_enrollments (
 entity_kind text NOT NULL CHECK(entity_kind IN ('drivers','trucks')),
 entity_id uuid NOT NULL,
 state text NOT NULL CHECK(state IN ('queued','running','review','unconfirmed','done','canceled')),
 version integer NOT NULL DEFAULT 1,
 environment text NOT NULL DEFAULT '',
 payload jsonb,
 receipt jsonb,
 last_error text NOT NULL DEFAULT '',
 sent_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(entity_kind,entity_id)
);
CREATE TABLE datatruck_enrollment_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 entity_kind text NOT NULL, entity_id uuid NOT NULL,
 snapshot jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION log_datatruck_enrollment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 INSERT INTO datatruck_enrollment_events(entity_kind,entity_id,snapshot) VALUES(NEW.entity_kind,NEW.entity_id,to_jsonb(NEW)); RETURN NEW;
END $$;
CREATE TRIGGER datatruck_enrollment_log AFTER INSERT OR UPDATE ON datatruck_enrollments FOR EACH ROW EXECUTE FUNCTION log_datatruck_enrollment();
-- Keep the existing service-task lifecycle and its audit history for DataTruck.
CREATE OR REPLACE FUNCTION sync_driver_service_task() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver_name text; label text;
BEGIN
 IF TG_OP='DELETE' THEN
  UPDATE system_task_records SET completed_at=coalesce(completed_at,now()),completed_by_name='System',outcome='Driver deleted' WHERE id=OLD.id;
  RETURN OLD;
 END IF;
 SELECT full_name INTO driver_name FROM drivers WHERE id=NEW.driver_id;
 label=CASE NEW.service WHEN 'relay' THEN 'Relay' WHEN 'datatruck' THEN 'DataTruck' ELSE 'WeighMyTruck' END;
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
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE datatruck_enrollments OWNER TO mserp_app;
  ALTER TABLE datatruck_enrollment_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
