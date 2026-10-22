BEGIN;

CREATE TABLE fleet_status_control (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), enabled boolean NOT NULL DEFAULT false);
INSERT INTO fleet_status_control DEFAULT VALUES;
ALTER TABLE drivers ADD COLUMN availability_status text NOT NULL DEFAULT 'active'
 CHECK(availability_status IN ('active','home','vacation','rest','reset','family_issue','health_issue','new_driver')),
 ADD COLUMN status_version integer NOT NULL DEFAULT 1;
UPDATE drivers SET availability_status=status WHERE status IN ('home','vacation');
ALTER TABLE trucks ADD COLUMN condition_status text NOT NULL DEFAULT 'ready', ADD COLUMN status_version integer NOT NULL DEFAULT 1;
UPDATE trucks SET condition_status=fleet_truck_operating_status(id);

CREATE TABLE fleet_status_rules (
 board_status text PRIMARY KEY, availability text NOT NULL DEFAULT '', truck_condition text NOT NULL DEFAULT '',
 requires_active boolean NOT NULL DEFAULT false, requires_ready boolean NOT NULL DEFAULT false, ordinal integer NOT NULL
);
INSERT INTO fleet_status_rules VALUES
 ('READY','active','',false,true,1),('REST','rest','',false,false,2),
 ('COVERED','','',true,true,3),('DISPATCHED','','',true,true,4),('ENROUTE','','',true,true,5),('RESERVED','','',true,true,6),
 ('VACATION','vacation','',false,false,7),('DEADHEAD','','',true,true,8),('HOME','home','',false,false,9),
 ('SHOP','','shop',false,false,10),('STOP','','out_of_service',false,false,11),('In the YARD','','',true,false,12),
 ('Hold for Load','','',true,false,13),('Left','','',false,false,14),('Family issue','family_issue','',false,false,15),
 ('Issue with PU/Del','','',true,true,16),('At PU','','',true,true,17),('Health issue','health_issue','',false,false,18),
 ('At DEL','','',true,true,19),('HOME (YARD)','home','',false,false,20),('NEW DRIVER','new_driver','',false,false,21),
 ('34 RESET','reset','',false,false,22),('ACCIDENT','','out_of_service',false,false,23),('HOLD','','hold',false,false,24);
ALTER TABLE driver_board DROP CONSTRAINT driver_board_status_check;
ALTER TABLE driver_board ADD CONSTRAINT driver_board_status_check CHECK(status IN (
 '', 'READY','REST','COVERED','DISPATCHED','ENROUTE','RESERVED','VACATION','DEADHEAD','HOME','SHOP','STOP','In the YARD',
 'Hold for Load','Left','Family issue','Issue with PU/Del','At PU','Health issue','At DEL','HOME (YARD)','NEW DRIVER','34 RESET','ACCIDENT','HOLD',
 'RESET','NO LOAD','STUCK','LATE DEL','TRUCK ISSUE','LEFT','LOAD CANCELLED','REJECTED'));

CREATE TABLE fleet_status_history (
 id bigserial PRIMARY KEY, entity_kind text NOT NULL CHECK(entity_kind IN ('driver','truck')), entity_id uuid NOT NULL,
 entity_name text NOT NULL, track text NOT NULL CHECK(track IN ('employment','availability','condition','dispatch')),
 previous_status text NOT NULL, status text NOT NULL, effective_at timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 actor_id text NOT NULL DEFAULT '', actor_name text NOT NULL, source text NOT NULL, source_ref text NOT NULL DEFAULT '',
 related_truck_id uuid, related_truck_name text NOT NULL DEFAULT '', action_id bigint NOT NULL DEFAULT txid_current(),
 baseline boolean NOT NULL DEFAULT false, reason text NOT NULL DEFAULT ''
);
CREATE INDEX fleet_status_history_entity ON fleet_status_history(entity_kind,entity_id,track,effective_at DESC,id DESC);
CREATE INDEX fleet_status_history_truck ON fleet_status_history(related_truck_id,effective_at);
CREATE FUNCTION protect_fleet_status_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Status history is append-only' USING ERRCODE='23514'; END $$;
CREATE TRIGGER fleet_status_history_immutable BEFORE UPDATE OR DELETE ON fleet_status_history FOR EACH ROW EXECUTE FUNCTION protect_fleet_status_history();

CREATE FUNCTION fleet_status_enabled() RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT enabled FROM fleet_status_control WHERE singleton $$;
CREATE FUNCTION fleet_status_lock() RETURNS void LANGUAGE sql AS $$ SELECT pg_advisory_xact_lock(137004137) $$;
CREATE FUNCTION fleet_status_statement_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF fleet_status_enabled() THEN PERFORM fleet_status_lock(); END IF; RETURN NULL; END $$;
CREATE TRIGGER fleet_status_lock BEFORE INSERT OR UPDATE OR DELETE ON driver_board FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();
CREATE TRIGGER fleet_status_lock BEFORE INSERT OR UPDATE OR DELETE ON drivers FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();
CREATE TRIGGER fleet_status_lock BEFORE INSERT OR UPDATE OR DELETE ON trucks FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();
CREATE TRIGGER fleet_status_lock BEFORE INSERT OR UPDATE OR DELETE ON maintenance_records FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();

-- Include secondary team drivers without creating a second physical assignment.
CREATE FUNCTION fleet_status_driver_truck(driver uuid) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT a.truck_id FROM truck_driver_assignments a WHERE a.driver_id=driver AND a.unassigned_at IS NULL
 UNION ALL SELECT a.truck_id FROM driver_teams team JOIN truck_driver_assignments a ON a.driver_id=team.main_driver_id AND a.unassigned_at IS NULL
 WHERE team.team_driver_id=driver AND team.start_week<=(now() AT TIME ZONE 'America/New_York')::date
 AND (team.end_week IS NULL OR team.end_week>(now() AT TIME ZONE 'America/New_York')::date) LIMIT 1
$$;
CREATE FUNCTION fleet_status_personal_label(availability text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE availability WHEN 'home' THEN 'HOME' WHEN 'vacation' THEN 'VACATION' WHEN 'rest' THEN 'REST' WHEN 'reset' THEN '34 RESET'
 WHEN 'family_issue' THEN 'Family issue' WHEN 'health_issue' THEN 'Health issue' WHEN 'new_driver' THEN 'NEW DRIVER' ELSE '' END
$$;

CREATE FUNCTION fleet_status_record(kind text, entity uuid, category text, previous text, following text, initial boolean DEFAULT false) RETURNS void LANGUAGE plpgsql AS $$
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
 coalesce(nullif(current_setting('mserp.board_source',true),''),'integration'),coalesce(current_setting('mserp.status_source_ref',true),''),truck,coalesce(truck_label,''),initial,coalesce(current_setting('mserp.status_reason',true),''));
END $$;

-- Projection writes cannot be mistaken for a fresh user status command.
CREATE FUNCTION fleet_status_project_driver(driver uuid, activate boolean DEFAULT false) RETURNS void LANGUAGE plpgsql AS $$
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
  label:=CASE condition WHEN 'shop' THEN 'SHOP' WHEN 'out_of_service' THEN 'STOP' WHEN 'hold' THEN 'HOLD' ELSE '' END;
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

CREATE FUNCTION fleet_status_driver_before() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.status IN ('home','vacation') THEN NEW.availability_status:=NEW.status; END IF; RETURN NEW; END IF;
 IF NEW.status IS DISTINCT FROM OLD.status THEN
  IF NEW.status IN ('home','vacation') THEN NEW.availability_status:=NEW.status;
  ELSIF NEW.status='active' AND OLD.status IN ('home','vacation','terminated') THEN NEW.availability_status:='active'; END IF;
 ELSIF NEW.availability_status IS DISTINCT FROM OLD.availability_status AND NEW.status<>'terminated' THEN
  NEW.status:=CASE WHEN NEW.availability_status IN ('home','vacation') THEN NEW.availability_status ELSE 'active' END;
 END IF;
 IF NEW.availability_status IS DISTINCT FROM OLD.availability_status OR NEW.active IS DISTINCT FROM OLD.active THEN NEW.status_version:=OLD.status_version+1; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER zz_fleet_status_driver_before BEFORE INSERT OR UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION fleet_status_driver_before();
CREATE FUNCTION fleet_status_driver_after() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT fleet_status_enabled() THEN RETURN NULL; END IF;
 IF TG_OP='INSERT' THEN
  PERFORM fleet_status_record('driver',NEW.id,'employment','',CASE WHEN NEW.active THEN 'active' ELSE 'terminated' END,true);
  PERFORM fleet_status_record('driver',NEW.id,'availability','',NEW.availability_status,true);
 ELSE
  IF NEW.active IS DISTINCT FROM OLD.active THEN PERFORM fleet_status_record('driver',NEW.id,'employment',CASE WHEN OLD.active THEN 'active' ELSE 'terminated' END,CASE WHEN NEW.active THEN 'active' ELSE 'terminated' END); END IF;
  IF NEW.availability_status IS DISTINCT FROM OLD.availability_status THEN
   PERFORM fleet_status_record('driver',NEW.id,'availability',OLD.availability_status,NEW.availability_status);
   PERFORM fleet_status_project_driver(NEW.id,true);
  END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER fleet_status_driver_after AFTER INSERT OR UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION fleet_status_driver_after();

CREATE FUNCTION fleet_status_refresh_truck(truck uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE previous text; following text; driver uuid;
BEGIN
 IF truck IS NULL OR NOT fleet_status_enabled() THEN RETURN; END IF;
 SELECT condition_status INTO previous FROM trucks WHERE id=truck FOR UPDATE;
 IF NOT FOUND THEN RETURN; END IF;
 following:=fleet_truck_operating_status(truck);
 IF previous IS DISTINCT FROM following THEN
  UPDATE trucks SET condition_status=following,status_version=status_version+1,updated_at=now() WHERE id=truck;
  PERFORM fleet_status_record('truck',truck,'condition',previous,following);
 END IF;
 FOR driver IN SELECT id FROM drivers WHERE active AND fleet_status_driver_truck(id)=truck ORDER BY id LOOP
  PERFORM fleet_status_project_driver(driver);
 END LOOP;
END $$;
CREATE FUNCTION fleet_status_truck_after() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT fleet_status_enabled() THEN RETURN NULL; END IF;
 IF TG_OP='INSERT' THEN PERFORM fleet_status_record('truck',NEW.id,'condition','',fleet_truck_operating_status(NEW.id),true); END IF;
 IF TG_OP='INSERT' OR NEW.status IS DISTINCT FROM OLD.status OR NEW.active IS DISTINCT FROM OLD.active THEN PERFORM fleet_status_refresh_truck(NEW.id); END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER fleet_status_truck_after AFTER INSERT OR UPDATE ON trucks FOR EACH ROW EXECUTE FUNCTION fleet_status_truck_after();
CREATE FUNCTION fleet_status_truck_before() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IS DISTINCT FROM OLD.status OR NEW.active IS DISTINCT FROM OLD.active THEN NEW.status_version:=OLD.status_version+1; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleet_status_truck_before BEFORE UPDATE ON trucks FOR EACH ROW EXECUTE FUNCTION fleet_status_truck_before();
CREATE FUNCTION fleet_status_work_order_after() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_ref text;
BEGIN
 old_ref:=coalesce(current_setting('mserp.status_source_ref',true),'');
 PERFORM set_config('mserp.status_source_ref',NEW.id::text,true);
 PERFORM fleet_status_refresh_truck(NEW.truck_id);
 IF TG_OP='UPDATE' AND OLD.truck_id IS DISTINCT FROM NEW.truck_id THEN PERFORM fleet_status_refresh_truck(OLD.truck_id); END IF;
 PERFORM set_config('mserp.status_source_ref',old_ref,true);
 RETURN NULL;
END $$;
CREATE TRIGGER fleet_status_work_order_after AFTER INSERT OR UPDATE OF dispatch_status,truck_id ON maintenance_records FOR EACH ROW EXECUTE FUNCTION fleet_status_work_order_after();

CREATE FUNCTION fleet_status_board_before() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rule fleet_status_rules; driver drivers; truck uuid; effective timestamptz; assigned timestamptz; guard text; condition text;
BEGIN
 IF NOT fleet_status_enabled() OR current_setting('mserp.status_projection',true)='1' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND NEW.status=OLD.status THEN RETURN NEW; END IF;
 IF NEW.status='' THEN RETURN NEW; END IF;
 SELECT * INTO rule FROM fleet_status_rules WHERE board_status=NEW.status;
 IF NOT FOUND THEN RETURN NEW; END IF; -- Existing retired values remain recoverable, never newly offered.
 IF NEW.status='Left' THEN RAISE EXCEPTION 'Use the explicit driver termination workflow' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 SELECT * INTO driver FROM drivers WHERE id=NEW.driver_id FOR UPDATE;
 truck:=fleet_status_driver_truck(NEW.driver_id);
 guard:=coalesce(current_setting('mserp.status_command',true),'');
 PERFORM set_config('mserp.status_command',NEW.driver_id::text,true);
 IF rule.requires_active AND (NOT driver.active OR driver.availability_status<>'active') THEN
  RAISE EXCEPTION 'Set the driver availability to Active before dispatch' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 IF rule.requires_ready AND truck IS NOT NULL AND EXISTS(SELECT 1 FROM trucks t WHERE t.id=truck AND (NOT active OR fleet_truck_operating_status(t.id)<>'ready')) THEN
  RAISE EXCEPTION 'Fleet must release the truck restriction before dispatch' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 IF rule.truck_condition<>'' THEN
  IF truck IS NULL THEN RAISE EXCEPTION 'Select an assigned truck before placing a restriction' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
  effective:=nullif(current_setting('mserp.status_effective_at',true),'')::timestamptz;
  SELECT max(a.assigned_at) INTO assigned FROM truck_driver_assignments a WHERE a.truck_id=truck AND a.unassigned_at IS NULL;
  IF effective<assigned THEN RAISE EXCEPTION 'A truck status cannot predate the current assignment' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
  -- This changes only the independent manual restriction. Other work-order holds remain authoritative.
  UPDATE trucks SET status=CASE WHEN status='out_of_service' THEN status WHEN status='hold' AND rule.truck_condition='shop' THEN status
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
CREATE TRIGGER fleet_status_board_before BEFORE INSERT OR UPDATE ON driver_board FOR EACH ROW EXECUTE FUNCTION fleet_status_board_before();
CREATE FUNCTION fleet_status_board_after() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF fleet_status_enabled() THEN
  IF TG_OP='INSERT' THEN PERFORM fleet_status_record('driver',NEW.driver_id,'dispatch','',NEW.status);
  ELSE PERFORM fleet_status_record('driver',NEW.driver_id,'dispatch',OLD.status,NEW.status); END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER fleet_status_board_after AFTER INSERT OR UPDATE ON driver_board FOR EACH ROW EXECUTE FUNCTION fleet_status_board_after();

-- Assignment changes reproject the new truck, never transfer its predecessor's restrictions.
CREATE FUNCTION fleet_status_assignment_after() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver uuid;
BEGIN
 IF NOT fleet_status_enabled() THEN RETURN NULL; END IF;
 IF TG_TABLE_NAME='driver_teams' THEN
  PERFORM fleet_status_project_driver(NEW.main_driver_id); PERFORM fleet_status_project_driver(NEW.team_driver_id);
 ELSE
  FOR driver IN SELECT id FROM drivers WHERE id=NEW.driver_id OR fleet_status_driver_truck(id)=NEW.truck_id LOOP PERFORM fleet_status_project_driver(driver); END LOOP;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER fleet_status_assignment_after AFTER INSERT OR UPDATE ON truck_driver_assignments FOR EACH ROW EXECUTE FUNCTION fleet_status_assignment_after();
CREATE TRIGGER fleet_status_team_after AFTER INSERT OR UPDATE ON driver_teams FOR EACH ROW EXECUTE FUNCTION fleet_status_assignment_after();

DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 ALTER TABLE fleet_status_control OWNER TO mserp_app; ALTER TABLE fleet_status_rules OWNER TO mserp_app;
 ALTER TABLE fleet_status_history OWNER TO mserp_app; ALTER SEQUENCE fleet_status_history_id_seq OWNER TO mserp_app;
END IF; END $$;
COMMIT;
