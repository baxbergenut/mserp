BEGIN;

-- Leave pre-existing attribution unknown: effective dates do not identify an editor.
ALTER TABLE truck_driver_assignments
 ADD COLUMN actor_id text NOT NULL DEFAULT '',
 ADD COLUMN actor_name text NOT NULL DEFAULT '',
 ADD COLUMN recorded_at timestamptz,
 ADD COLUMN ended_by_id text NOT NULL DEFAULT '',
 ADD COLUMN ended_by_name text NOT NULL DEFAULT '',
 ADD COLUMN ended_recorded_at timestamptz;
ALTER TABLE driver_dispatcher_assignments
 ADD COLUMN actor_id text NOT NULL DEFAULT '',
 ADD COLUMN actor_name text NOT NULL DEFAULT '',
 ADD COLUMN recorded_at timestamptz,
 ADD COLUMN ended_by_id text NOT NULL DEFAULT '',
 ADD COLUMN ended_by_name text NOT NULL DEFAULT '',
 ADD COLUMN ended_recorded_at timestamptz;

-- The same transaction-local, server-owned actor context as status history.
-- Names are snapshots, with no user FK so later renames/deletions retain history.
CREATE FUNCTION record_assignment_actor() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 actor text := coalesce(current_setting('mserp.board_actor',true),'');
 actor_label text;
BEGIN
 IF actor<>'' THEN
  SELECT username INTO actor_label FROM app_users WHERE id::text=actor;
 END IF;
 actor_label := coalesce(actor_label,CASE WHEN actor='' THEN 'System' ELSE 'Unknown' END);
 IF TG_OP='INSERT' THEN
  NEW.actor_id := actor;
  NEW.actor_name := actor_label;
  NEW.recorded_at := clock_timestamp();
 ELSE
  NEW.actor_id := OLD.actor_id;
  NEW.actor_name := OLD.actor_name;
  NEW.recorded_at := OLD.recorded_at;
 END IF;
 IF TG_OP='INSERT' OR NEW.unassigned_at IS DISTINCT FROM OLD.unassigned_at THEN
  NEW.ended_by_id := CASE WHEN NEW.unassigned_at IS NULL THEN '' ELSE actor END;
  NEW.ended_by_name := CASE WHEN NEW.unassigned_at IS NULL THEN '' ELSE actor_label END;
  NEW.ended_recorded_at := CASE WHEN NEW.unassigned_at IS NULL THEN NULL ELSE clock_timestamp() END;
 ELSE
  NEW.ended_by_id := OLD.ended_by_id;
  NEW.ended_by_name := OLD.ended_by_name;
  NEW.ended_recorded_at := OLD.ended_recorded_at;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER assignment_actor BEFORE INSERT OR UPDATE ON truck_driver_assignments
 FOR EACH ROW EXECUTE FUNCTION record_assignment_actor();
CREATE TRIGGER assignment_actor BEFORE INSERT OR UPDATE ON driver_dispatcher_assignments
 FOR EACH ROW EXECUTE FUNCTION record_assignment_actor();

COMMIT;
