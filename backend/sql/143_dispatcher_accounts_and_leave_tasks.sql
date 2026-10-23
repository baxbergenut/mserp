BEGIN;

ALTER TABLE app_roles ADD COLUMN dispatcher_role boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX app_roles_dispatcher_role_idx ON app_roles(dispatcher_role) WHERE dispatcher_role;
UPDATE app_roles SET dispatcher_role=true WHERE name='Dispatcher' AND NOT system_role;
INSERT INTO app_roles(name,permissions,dispatcher_role)
SELECT 'Dispatcher',ARRAY['driver_board.read','driver_board.write','board.read','board.write','drivers.read','trucks.read','loads.read','tasks.read'],true
WHERE NOT EXISTS(SELECT 1 FROM app_roles WHERE dispatcher_role);

CREATE FUNCTION protect_dispatcher_role() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.dispatcher_role AND (TG_OP='DELETE' OR NEW.name IS DISTINCT FROM OLD.name OR NOT NEW.dispatcher_role OR NEW.system_role) THEN
  RAISE EXCEPTION 'The built-in Dispatcher role cannot be removed or renamed' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_dispatcher_role BEFORE UPDATE OR DELETE ON app_roles FOR EACH ROW EXECUTE FUNCTION protect_dispatcher_role();

ALTER TABLE dispatchers ADD COLUMN user_account_id uuid UNIQUE REFERENCES app_users(id) ON DELETE RESTRICT;
CREATE FUNCTION sync_dispatcher_account() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pg_trigger_depth()>1 THEN RETURN NEW; END IF;
 IF TG_TABLE_NAME='dispatchers' THEN
  IF TG_OP='UPDATE' AND OLD.user_account_id IS NOT NULL AND NEW.user_account_id IS DISTINCT FROM OLD.user_account_id THEN
   RAISE EXCEPTION 'An existing dispatcher account cannot be replaced' USING ERRCODE='23514';
  END IF;
  IF NEW.user_account_id IS NOT NULL THEN
   IF NOT EXISTS(SELECT 1 FROM app_users u JOIN app_roles r ON r.id=u.role_id WHERE u.id=NEW.user_account_id AND r.dispatcher_role) THEN
    RAISE EXCEPTION 'Dispatcher accounts must use the built-in Dispatcher role' USING ERRCODE='23514';
   END IF;
   UPDATE app_users SET username=NEW.full_name,email=NEW.email,active=NEW.active,version=version+1,updated_at=now()
   WHERE id=NEW.user_account_id AND (username,email,active) IS DISTINCT FROM (NEW.full_name,NEW.email,NEW.active);
   IF FOUND THEN DELETE FROM auth_sessions WHERE user_id=NEW.user_account_id; END IF;
  END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM dispatchers WHERE user_account_id=NEW.id) THEN
   IF NOT EXISTS(SELECT 1 FROM app_roles WHERE id=NEW.role_id AND dispatcher_role) THEN
    RAISE EXCEPTION 'Dispatcher accounts must keep the built-in Dispatcher role' USING ERRCODE='23514',CONSTRAINT='dispatcher_account_role';
   END IF;
   UPDATE dispatchers SET full_name=NEW.username,normalized_name=lower(regexp_replace(btrim(NEW.username),'\s+',' ','g')),email=NEW.email,active=NEW.active,updated_at=now()
   WHERE user_account_id=NEW.id AND (full_name,email,active) IS DISTINCT FROM (NEW.username,NEW.email,NEW.active);
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER dispatcher_account_sync AFTER INSERT OR UPDATE ON dispatchers FOR EACH ROW EXECUTE FUNCTION sync_dispatcher_account();
CREATE TRIGGER account_dispatcher_sync AFTER UPDATE ON app_users FOR EACH ROW EXECUTE FUNCTION sync_dispatcher_account();

ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release','relay_setup','weighmytruck_setup','datatruck_setup','five_eld_truck','maintenance_pm','maintenance_program','driver_leave_return'));
INSERT INTO system_task_assignments(kind) VALUES('driver_leave_return');
ALTER TABLE system_task_records ADD COLUMN leave_dispatcher_id uuid REFERENCES dispatchers(id) ON DELETE SET NULL;
ALTER TABLE system_task_records ADD COLUMN leave_key text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX system_task_leave_open_idx ON system_task_records(service_driver_id) WHERE kind='driver_leave_return' AND completed_at IS NULL;
CREATE TRIGGER dispatcher_tasks_changed AFTER UPDATE OR DELETE ON dispatchers FOR EACH STATEMENT EXECUTE FUNCTION notify_task_change();

COMMIT;
