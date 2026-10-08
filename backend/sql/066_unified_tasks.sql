BEGIN;

ALTER TABLE custom_tasks ADD COLUMN completed_by UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE custom_tasks ADD COLUMN completed_by_name TEXT NOT NULL DEFAULT '';

-- Source identities deliberately have no FK: completed work survives source deletion.
CREATE TABLE system_task_records (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL REFERENCES system_task_assignments(kind),
    title TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    completed_by_name TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL DEFAULT ''
);
CREATE INDEX system_task_records_status_idx ON system_task_records((completed_at IS NOT NULL),created_at DESC,id);

INSERT INTO system_task_records(id,kind,title,created_at,completed_at,completed_by_name,outcome)
SELECT i.id,'driver_onboarding','Set up ' || coalesce(i.driver_data->>'fullName','driver'),i.received_at,
 coalesce(i.completed_at,i.terminated_at),coalesce(u.username,CASE WHEN i.terminated_at IS NOT NULL AND i.completed_at IS NULL THEN 'System' ELSE '' END),
 CASE WHEN i.completed_at IS NULL AND i.terminated_at IS NOT NULL THEN 'Cancelled by termination' ELSE '' END
FROM fleetscope_driver_intake i LEFT JOIN app_users u ON u.id=i.completed_by;

INSERT INTO system_task_records(id,kind,title,created_at,completed_at,completed_by_name)
SELECT l.id,'relay_review','Review Relay account: ' || coalesce(nullif(concat_ws(' ',l.relay_first_name,l.relay_last_name),''),l.relay_driver_id),
 l.created_at,CASE WHEN l.driver_id IS NOT NULL THEN v.reviewed_at END,coalesce(u.username,'')
FROM relay_driver_links l
LEFT JOIN LATERAL (SELECT reviewed_at,reviewed_by FROM relay_identity_reviews WHERE identity_id=l.id AND action='link' ORDER BY reviewed_at DESC LIMIT 1) v ON true
LEFT JOIN app_users u ON u.id=v.reviewed_by
WHERE l.driver_id IS NULL OR v.reviewed_at IS NOT NULL;

CREATE FUNCTION sync_intake_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO system_task_records(id,kind,title,created_at,completed_at,completed_by_name,outcome)
 VALUES(NEW.id,'driver_onboarding','Set up ' || coalesce(NEW.driver_data->>'fullName','driver'),NEW.received_at,
 coalesce(NEW.completed_at,NEW.terminated_at),coalesce((SELECT username FROM app_users WHERE id=NEW.completed_by),CASE WHEN NEW.terminated_at IS NOT NULL AND NEW.completed_at IS NULL THEN 'System' ELSE '' END),
 CASE WHEN NEW.completed_at IS NULL AND NEW.terminated_at IS NOT NULL THEN 'Cancelled by termination' ELSE '' END)
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,completed_at=excluded.completed_at,
 completed_by_name=CASE WHEN system_task_records.completed_at IS NOT NULL THEN system_task_records.completed_by_name ELSE excluded.completed_by_name END,outcome=excluded.outcome;
 RETURN NEW;
END $$;
CREATE TRIGGER intake_task AFTER INSERT OR UPDATE ON fleetscope_driver_intake FOR EACH ROW EXECUTE FUNCTION sync_intake_task();

CREATE FUNCTION sync_relay_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.driver_id IS NULL THEN
  INSERT INTO system_task_records(id,kind,title,notes,created_at)
  VALUES(NEW.id,'relay_review','Review Relay account: ' || coalesce(nullif(concat_ws(' ',NEW.relay_first_name,NEW.relay_last_name),''),NEW.relay_driver_id),concat_ws(' · ',NEW.relay_email,NEW.relay_phone,NEW.relay_driver_id),NEW.created_at)
  ON CONFLICT(id) DO UPDATE SET title=excluded.title,notes=excluded.notes;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER relay_task AFTER INSERT OR UPDATE ON relay_driver_links FOR EACH ROW EXECUTE FUNCTION sync_relay_task();

CREATE FUNCTION complete_relay_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.action='link' THEN
  UPDATE system_task_records SET completed_at=NEW.reviewed_at,
   completed_by_name=coalesce((SELECT username FROM app_users WHERE id=NEW.reviewed_by),'')
   WHERE id=NEW.identity_id AND completed_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER relay_task_completed AFTER INSERT ON relay_identity_reviews FOR EACH ROW EXECUTE FUNCTION complete_relay_task();

-- One committed notification per transaction wakes the shared browser stream.
CREATE FUNCTION notify_task_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
 PERFORM pg_notify('mserp_tasks','');
 RETURN NULL;
END $$;
CREATE TRIGGER custom_task_changed AFTER INSERT OR UPDATE OR DELETE ON custom_tasks FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER system_task_changed AFTER INSERT OR UPDATE OR DELETE ON system_task_records FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER task_assignment_changed AFTER UPDATE ON system_task_assignments FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER task_user_changed AFTER UPDATE OR DELETE ON app_users FOR EACH ROW EXECUTE FUNCTION notify_task_change();
CREATE TRIGGER task_role_changed AFTER UPDATE OR DELETE ON app_roles FOR EACH ROW EXECUTE FUNCTION notify_task_change();

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE system_task_records OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
