BEGIN;
SELECT fleet_status_lock();
CREATE TEMP TABLE truck_status_conversion ON COMMIT DROP AS SELECT id,to_jsonb(t) AS before_value FROM trucks t;
CREATE TEMP TABLE truck_status_activation ON COMMIT DROP AS SELECT enabled FROM fleet_status_control;
UPDATE fleet_status_control SET enabled=false;
ALTER TABLE trucks ADD COLUMN status_reason text NOT NULL DEFAULT '';
ALTER TABLE trucks DROP CONSTRAINT trucks_status_check;
UPDATE trucks SET status_reason=CASE WHEN NOT active THEN 'Migrated from inactive; original departure reason may be recorded in notes.' WHEN status='hold' THEN 'Migrated from Hold; original reason may be recorded in notes.' WHEN status IN ('maintenance','out_of_service') THEN coalesce(nullif((SELECT reason FROM fleet_status_history h WHERE h.entity_kind='truck' AND h.entity_id=trucks.id AND reason<>'' ORDER BY id DESC LIMIT 1),''),'Existing restriction; original reason not recorded.') ELSE '' END,
 status=CASE WHEN NOT active THEN 'terminated' WHEN status='hold' THEN 'maintenance' ELSE status END;
UPDATE maintenance_records SET dispatch_status='shop' WHERE dispatch_status='hold';
ALTER TABLE trucks ADD CONSTRAINT trucks_status_check CHECK(status IN ('available','assigned','maintenance','out_of_service','terminated'));
ALTER TABLE maintenance_records DROP CONSTRAINT maintenance_records_dispatch_status_check;
ALTER TABLE maintenance_records ADD CONSTRAINT maintenance_records_dispatch_status_check CHECK(dispatch_status IN ('','shop','out_of_service'));
UPDATE fleet_status_rules SET truck_condition='shop' WHERE board_status IN ('STOP','ACCIDENT');
DELETE FROM fleet_status_rules WHERE board_status='HOLD';

CREATE OR REPLACE FUNCTION fleet_status_statement_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM fleet_status_lock(); RETURN NULL; END $$;

CREATE OR REPLACE FUNCTION fleet_status_record(kind text, entity uuid, category text, previous text, following text, initial boolean DEFAULT false) RETURNS void LANGUAGE plpgsql AS $$
DECLARE actor text; author text; entity_label text; truck uuid; truck_label text; effective timestamptz; last_at timestamptz;
BEGIN
 IF previous=following AND NOT initial THEN RETURN; END IF;
 actor:=coalesce(current_setting('mserp.board_actor',true),'');
 SELECT username INTO author FROM app_users WHERE id::text=actor;
 author:=coalesce(author,nullif(current_setting('mserp.status_actor_name',true),''),'System');
 IF kind='driver' THEN SELECT full_name INTO entity_label FROM drivers WHERE id=entity; truck:=fleet_status_driver_truck(entity);
 ELSE SELECT unit_number INTO entity_label FROM trucks WHERE id=entity; truck:=entity; END IF;
 SELECT unit_number INTO truck_label FROM trucks WHERE id=truck;
 effective:=coalesce(nullif(current_setting('mserp.status_effective_at',true),'')::timestamptz,statement_timestamp());
 SELECT max(effective_at) INTO last_at FROM fleet_status_history WHERE entity_kind=kind AND entity_id=entity AND track=category;
 IF effective>clock_timestamp() OR (last_at IS NOT NULL AND effective<last_at) THEN
  RAISE EXCEPTION 'Effective time must follow the previous status and cannot be in the future' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid';
 END IF;
 INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_id,actor_name,source,source_ref,related_truck_id,related_truck_name,baseline,reason)
 VALUES(kind,entity,coalesce(entity_label,''),category,previous,following,effective,actor,author,
 coalesce(nullif(current_setting('mserp.board_source',true),''),'integration'),coalesce(current_setting('mserp.status_source_ref',true),''),truck,coalesce(truck_label,''),initial,coalesce(current_setting('mserp.status_reason',true),''))
 ON CONFLICT(entity_kind,entity_id,track,action_id) DO UPDATE SET status=excluded.status,
 related_truck_id=excluded.related_truck_id,related_truck_name=excluded.related_truck_name,reason=excluded.reason,source=excluded.source,source_ref=excluded.source_ref;
 DELETE FROM fleet_status_history WHERE entity_kind=kind AND entity_id=entity AND track=category AND action_id=txid_current() AND previous_status=status AND NOT baseline;
END $$;

CREATE FUNCTION fleet_truck_has_operator(truck uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM drivers d WHERE d.active AND fleet_status_driver_truck(d.id)=truck)
$$;
CREATE OR REPLACE FUNCTION fleet_truck_operating_status(truck uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN t.status='terminated' THEN 'terminated'
 WHEN t.status='out_of_service' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='out_of_service') THEN 'out_of_service'
 WHEN t.status='maintenance' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='shop') THEN 'shop'
 WHEN fleet_truck_has_operator(t.id) THEN 'in_use' ELSE 'ready' END FROM trucks t WHERE t.id=truck
$$;
CREATE FUNCTION fleet_truck_status_reason(truck uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT concat_ws(E'\n',nullif(t.status_reason,''),(SELECT string_agg('Work order: '||coalesce(nullif(btrim(m.notes),''),'Existing restriction; original reason not recorded.'),E'\n' ORDER BY m.id) FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status<>'')) FROM trucks t WHERE t.id=truck
$$;
CREATE OR REPLACE FUNCTION fleet_status_truck_before() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE reason text; changed boolean;
BEGIN
 changed:=TG_OP='INSERT';
 IF TG_OP='UPDATE' THEN changed:=NEW.status IS DISTINCT FROM OLD.status; END IF;
 IF changed THEN
  reason:=btrim(coalesce(nullif(current_setting('mserp.status_reason',true),''),CASE WHEN TG_OP='INSERT' OR NEW.status_reason IS DISTINCT FROM OLD.status_reason THEN NEW.status_reason END,''));
  IF NEW.status IN ('maintenance','out_of_service','terminated') OR (TG_OP='UPDATE' AND OLD.status IN ('maintenance','out_of_service','terminated')) THEN
   IF reason='' OR length(reason)>1000 THEN RAISE EXCEPTION 'A truck status reason is required (maximum 1000 characters)' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
   IF NEW.status='terminated' AND fleet_truck_has_operator(NEW.id) THEN RAISE EXCEPTION 'Reassign employed operators before terminating this truck' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
   IF NEW.status IN ('available','assigned') AND EXISTS(SELECT 1 FROM maintenance_records WHERE truck_id=NEW.id AND dispatch_status<>'') THEN RAISE EXCEPTION 'Release remaining work-order restrictions first' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
   NEW.status_reason:=CASE WHEN NEW.status IN ('available','assigned') THEN '' ELSE reason END;
  END IF;
  IF TG_OP='UPDATE' THEN NEW.status_version:=OLD.status_version+1; END IF;
 END IF;
 NEW.active:=NEW.status NOT IN ('out_of_service','terminated') AND NOT EXISTS(SELECT 1 FROM maintenance_records WHERE truck_id=NEW.id AND dispatch_status='out_of_service');
 RETURN NEW;
END $$;
DROP TRIGGER fleet_status_truck_before ON trucks;
CREATE TRIGGER fleet_status_truck_before BEFORE INSERT OR UPDATE ON trucks FOR EACH ROW EXECUTE FUNCTION fleet_status_truck_before();

CREATE OR REPLACE FUNCTION fleet_status_refresh_truck(truck uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE previous text; following text; driver uuid; old_reason text;
BEGIN
 IF truck IS NULL THEN RETURN; END IF;
 SELECT condition_status INTO previous FROM trucks WHERE id=truck FOR UPDATE;
 IF NOT FOUND THEN RETURN; END IF;
 following:=fleet_truck_operating_status(truck);
 IF previous IS DISTINCT FROM following THEN
  UPDATE trucks SET condition_status=following,active=following NOT IN ('out_of_service','terminated'),status_version=status_version+1,updated_at=now() WHERE id=truck;
  IF fleet_status_enabled() THEN
   old_reason:=coalesce(current_setting('mserp.status_reason',true),'');
   IF old_reason='' THEN PERFORM set_config('mserp.status_reason',CASE WHEN following IN ('ready','in_use') THEN 'Driver assignment or employment changed' ELSE fleet_truck_status_reason(truck) END,true); END IF;
   PERFORM fleet_status_record('truck',truck,'condition',previous,following);
   PERFORM set_config('mserp.status_reason',old_reason,true);
  END IF;
 END IF;
 FOR driver IN SELECT id FROM drivers WHERE active AND fleet_status_driver_truck(id)=truck ORDER BY id LOOP PERFORM fleet_status_project_driver(driver); END LOOP;
END $$;
CREATE OR REPLACE FUNCTION fleet_status_truck_after() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  PERFORM fleet_status_refresh_truck(NEW.id);
  IF fleet_status_enabled() THEN PERFORM fleet_status_record('truck',NEW.id,'condition','',fleet_truck_operating_status(NEW.id),true); END IF;
 ELSIF NEW.status IS DISTINCT FROM OLD.status THEN PERFORM fleet_status_refresh_truck(NEW.id);
 END IF;
 RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION fleet_status_project_driver(driver uuid, activate boolean DEFAULT false) RETURNS void LANGUAGE plpgsql AS $$
DECLARE availability text; condition text; label text; old_label text; guard text;
BEGIN
 IF NOT fleet_status_enabled() OR current_setting('mserp.status_command',true)=driver::text THEN RETURN; END IF;
 IF payroll_driver_at(driver,(now() AT TIME ZONE 'America/New_York')::date)<>driver THEN RETURN; END IF;
 SELECT availability_status INTO availability FROM drivers WHERE id=driver AND active;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT fleet_truck_operating_status(fleet_status_driver_truck(driver)) INTO condition;
 SELECT status INTO old_label FROM driver_board WHERE driver_id=driver;
 label:=fleet_status_personal_label(availability);
 IF label='' THEN
  label:=CASE condition WHEN 'shop' THEN 'SHOP' WHEN 'out_of_service' THEN 'STOP' WHEN 'terminated' THEN 'STOP' ELSE '' END;
  IF label='' THEN
   IF activate OR old_label IN ('SHOP','STOP','ACCIDENT','HOLD','HOME','HOME (YARD)','VACATION','REST','34 RESET','Family issue','Health issue','NEW DRIVER') THEN label:='READY';
   ELSE RETURN; END IF;
  ELSIF old_label='ACCIDENT' AND condition='out_of_service' THEN label:='ACCIDENT'; END IF;
 END IF;
 IF old_label IS NOT DISTINCT FROM label THEN RETURN; END IF;
 guard:=coalesce(current_setting('mserp.status_projection',true),'');
 PERFORM set_config('mserp.status_projection','1',true);
 INSERT INTO driver_board(driver_id,status) VALUES(driver,label) ON CONFLICT(driver_id) DO UPDATE SET status=excluded.status,version=driver_board.version+1,updated_at=now();
 PERFORM set_config('mserp.status_projection',guard,true);
END $$;
CREATE OR REPLACE FUNCTION fleet_status_board_before() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rule fleet_status_rules; driver drivers; truck uuid; effective timestamptz; assigned timestamptz; guard text; condition text;
BEGIN
 IF NOT fleet_status_enabled() OR current_setting('mserp.status_projection',true)='1' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND NEW.status=OLD.status THEN RETURN NEW; END IF;
 IF NEW.status='' THEN RETURN NEW; END IF;
 SELECT * INTO rule FROM fleet_status_rules WHERE board_status=NEW.status;
 IF NOT FOUND THEN IF NEW.status='HOLD' THEN RAISE EXCEPTION 'Use Shop with a reason' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF; RETURN NEW; END IF;
 IF NEW.status='Left' THEN RAISE EXCEPTION 'Use the explicit driver termination workflow' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 SELECT * INTO driver FROM drivers WHERE id=NEW.driver_id FOR UPDATE;
 truck:=fleet_status_driver_truck(NEW.driver_id);
 guard:=coalesce(current_setting('mserp.status_command',true),'');
 PERFORM set_config('mserp.status_command',NEW.driver_id::text,true);
 IF rule.requires_active AND (NOT driver.active OR driver.availability_status<>'active') THEN
  RAISE EXCEPTION 'Set the driver availability to Active before dispatch' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 IF rule.requires_ready AND truck IS NOT NULL AND EXISTS(SELECT 1 FROM trucks t WHERE t.id=truck AND (NOT active OR fleet_truck_operating_status(t.id) NOT IN ('ready','in_use'))) THEN
  RAISE EXCEPTION 'Fleet must release the truck restriction before dispatch' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 IF rule.truck_condition<>'' THEN
  IF truck IS NULL THEN RAISE EXCEPTION 'Select an assigned truck before placing a restriction' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
  effective:=nullif(current_setting('mserp.status_effective_at',true),'')::timestamptz;
  SELECT max(a.assigned_at) INTO assigned FROM truck_driver_assignments a WHERE a.truck_id=truck AND a.unassigned_at IS NULL;
  IF effective<assigned THEN RAISE EXCEPTION 'A truck status cannot predate the current assignment' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
  IF btrim(coalesce(current_setting('mserp.status_reason',true),''))='' THEN RAISE EXCEPTION 'A truck status reason is required' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
  -- Independent restrictions survive board commands.
  UPDATE trucks SET status=CASE WHEN status IN ('out_of_service','terminated') THEN status
   WHEN rule.truck_condition='shop' THEN 'maintenance' ELSE rule.truck_condition END,updated_at=now() WHERE id=truck;
  condition:=fleet_truck_operating_status(truck);
  NEW.status:=CASE condition WHEN 'shop' THEN 'SHOP' WHEN 'hold' THEN 'HOLD' ELSE CASE WHEN NEW.status='ACCIDENT' THEN 'ACCIDENT' ELSE 'STOP' END END;
  IF driver.availability_status<>'active' THEN NEW.status:=fleet_status_personal_label(driver.availability_status); END IF;
 END IF;
 IF rule.availability<>'' AND rule.availability IS DISTINCT FROM driver.availability_status THEN
  UPDATE drivers SET availability_status=rule.availability,updated_at=now() WHERE id=NEW.driver_id;
 END IF;
 PERFORM set_config('mserp.status_command',guard,true);
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION fleet_status_assignment_after() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE truck uuid;
BEGIN
 IF TG_OP='DELETE' THEN
  IF TG_TABLE_NAME='driver_teams' THEN
   FOR truck IN SELECT DISTINCT a.truck_id FROM truck_driver_assignments a WHERE a.unassigned_at IS NULL AND a.driver_id IN (OLD.main_driver_id,OLD.team_driver_id) LOOP PERFORM fleet_status_refresh_truck(truck); END LOOP;
   PERFORM fleet_status_project_driver(OLD.main_driver_id); PERFORM fleet_status_project_driver(OLD.team_driver_id);
  ELSE PERFORM fleet_status_refresh_truck(OLD.truck_id); PERFORM fleet_status_project_driver(OLD.driver_id); END IF;
  RETURN NULL;
 END IF;
 IF TG_TABLE_NAME='driver_teams' THEN
  FOR truck IN SELECT DISTINCT a.truck_id FROM truck_driver_assignments a WHERE a.unassigned_at IS NULL AND a.driver_id IN (NEW.main_driver_id,NEW.team_driver_id) LOOP PERFORM fleet_status_refresh_truck(truck); END LOOP;
  IF TG_OP='UPDATE' THEN
   FOR truck IN SELECT DISTINCT a.truck_id FROM truck_driver_assignments a WHERE a.unassigned_at IS NULL AND a.driver_id IN (OLD.main_driver_id,OLD.team_driver_id) LOOP PERFORM fleet_status_refresh_truck(truck); END LOOP;
  END IF;
  IF TG_OP='UPDATE' THEN PERFORM fleet_status_project_driver(OLD.main_driver_id); PERFORM fleet_status_project_driver(OLD.team_driver_id); END IF;
  PERFORM fleet_status_project_driver(NEW.main_driver_id); PERFORM fleet_status_project_driver(NEW.team_driver_id);
 ELSE
  PERFORM fleet_status_refresh_truck(NEW.truck_id);
  IF TG_OP='UPDATE' AND OLD.truck_id IS DISTINCT FROM NEW.truck_id THEN PERFORM fleet_status_refresh_truck(OLD.truck_id); END IF;
  IF TG_OP='UPDATE' AND NEW.driver_id IS DISTINCT FROM OLD.driver_id THEN PERFORM fleet_status_project_driver(OLD.driver_id); END IF;
  PERFORM fleet_status_project_driver(NEW.driver_id);
 END IF;
 RETURN NULL;
END $$;
CREATE FUNCTION fleet_truck_employment_after() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE truck uuid;
BEGIN
 IF NEW.active IS DISTINCT FROM OLD.active THEN
  FOR truck IN SELECT t.id FROM trucks t WHERE t.id=fleet_status_driver_truck(NEW.id) LOOP PERFORM fleet_status_refresh_truck(truck); END LOOP;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER fleet_truck_employment_after AFTER UPDATE OF active ON drivers FOR EACH ROW EXECUTE FUNCTION fleet_truck_employment_after();
CREATE FUNCTION fleet_truck_assignment_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.unassigned_at IS NULL AND (TG_OP='INSERT' OR OLD.unassigned_at IS NOT NULL OR NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.driver_id IS DISTINCT FROM OLD.driver_id) THEN
  PERFORM id FROM trucks WHERE id=NEW.truck_id FOR UPDATE;
  IF fleet_truck_operating_status(NEW.truck_id) NOT IN ('ready','in_use') THEN RAISE EXCEPTION 'Return the truck to service before assigning a driver' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleet_status_lock BEFORE INSERT OR UPDATE OR DELETE ON truck_driver_assignments FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();
CREATE TRIGGER fleet_truck_assignment_guard BEFORE INSERT OR UPDATE ON truck_driver_assignments FOR EACH ROW EXECUTE FUNCTION fleet_truck_assignment_guard();
CREATE FUNCTION fleet_truck_work_reason_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.dispatch_status<>'' AND (TG_OP='INSERT' OR NEW.dispatch_status IS DISTINCT FROM OLD.dispatch_status) AND btrim(coalesce(NEW.notes,''))='' THEN
  RAISE EXCEPTION 'Specify the reason in work-order notes before restricting a truck' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleet_truck_work_reason_guard BEFORE INSERT OR UPDATE ON maintenance_records FOR EACH ROW EXECUTE FUNCTION fleet_truck_work_reason_guard();

DROP TRIGGER fleet_status_assignment_after ON truck_driver_assignments;
CREATE TRIGGER fleet_status_assignment_after AFTER INSERT OR UPDATE OR DELETE ON truck_driver_assignments FOR EACH ROW EXECUTE FUNCTION fleet_status_assignment_after();
DROP TRIGGER fleet_status_team_after ON driver_teams;
CREATE TRIGGER fleet_status_team_after AFTER INSERT OR UPDATE OR DELETE ON driver_teams FOR EACH ROW EXECUTE FUNCTION fleet_status_assignment_after();
CREATE TRIGGER fleet_status_lock BEFORE INSERT OR UPDATE OR DELETE ON driver_teams FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();
CREATE FUNCTION fleet_truck_team_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE truck uuid;
BEGIN
 IF NEW.start_week<=(now() AT TIME ZONE 'America/New_York')::date AND (NEW.end_week IS NULL OR NEW.end_week>(now() AT TIME ZONE 'America/New_York')::date)
 AND (TG_OP='INSERT' OR NEW.main_driver_id IS DISTINCT FROM OLD.main_driver_id OR NEW.team_driver_id IS DISTINCT FROM OLD.team_driver_id OR NEW.start_week IS DISTINCT FROM OLD.start_week OR (OLD.end_week IS NOT NULL AND OLD.end_week<=(now() AT TIME ZONE 'America/New_York')::date)) THEN
  SELECT a.truck_id INTO truck FROM truck_driver_assignments a WHERE a.driver_id=NEW.main_driver_id AND a.unassigned_at IS NULL;
  PERFORM id FROM trucks WHERE id=truck FOR UPDATE;
  IF fleet_truck_operating_status(truck) NOT IN ('ready','in_use') THEN RAISE EXCEPTION 'Return the truck to service before assigning a team driver' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleet_truck_team_guard BEFORE INSERT OR UPDATE ON driver_teams FOR EACH ROW EXECUTE FUNCTION fleet_truck_team_guard();

UPDATE trucks SET condition_status=fleet_truck_operating_status(id),active=fleet_truck_operating_status(id) NOT IN ('out_of_service','terminated');
UPDATE fleet_status_control SET enabled=(SELECT enabled FROM truck_status_activation);
SELECT set_config('mserp.board_source','truck_status_migration',true),set_config('mserp.status_reason','Approved status conversion; earlier status start unknown. Original notes and history retained.',true);
SELECT fleet_status_record('truck',t.id,'condition',coalesce(b.before_value->>'condition_status',''),t.condition_status,true) FROM trucks t JOIN truck_status_conversion b USING(id);
SELECT fleet_status_project_driver(id) FROM drivers WHERE active;
COMMIT;
