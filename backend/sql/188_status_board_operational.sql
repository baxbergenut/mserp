-- Status Board is operational. Canonical driver/truck changes still enqueue
-- payroll work, but board text, load selections and their audit are not inputs.
BEGIN;
UPDATE payroll_projection_sources SET classification='unrelated'
WHERE table_name IN ('driver_board','driver_board_history','driver_board_load_state');
DO $$ DECLARE t text; trigger_name text; BEGIN
 FOREACH t IN ARRAY ARRAY['driver_board','driver_board_history','driver_board_load_state'] LOOP
  FOREACH trigger_name IN ARRAY ARRAY['payroll_protocol_statement','payroll_protocol_row',
   'payroll_projection_insert','payroll_projection_update','payroll_projection_delete','payroll_projection_truncate'] LOOP
   EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I',trigger_name,t);
  END LOOP;
 END LOOP;
END $$;

-- Team configuration already takes the fleet lock before changing membership.
-- A board edit needs that lock for its main-driver check, not the payroll gate.
DO $$ DECLARE definition text; BEGIN
 definition:=replace(pg_get_functiondef('guard_team_dispatch()'::regprocedure),E'\r\n',E'\n');
 IF strpos(definition,'PERFORM payroll_financial_gate(true);')=0 THEN RAISE EXCEPTION 'Unexpected team dispatch guard'; END IF;
 EXECUTE replace(definition,'PERFORM payroll_financial_gate(true);',
  'IF TG_TABLE_NAME=''driver_board'' THEN PERFORM fleet_status_lock(); ELSE PERFORM payroll_financial_gate(true); END IF;');
END $$;

-- Operational commands may run beside payroll calculations. If a status really
-- changes a canonical profile, abort before taking any payroll lock; the caller
-- retries the original versions once under source -> fleet -> driver locks.
DO $$ DECLARE definition text; prefix text; BEGIN
 prefix:=$prefix$BEGIN
 IF current_setting('mserp.payroll.board_operational',true)='on' THEN
  IF TG_TABLE_NAME='fleet_status_history' AND TG_OP<>'TRUNCATE' THEN RETURN NULL; END IF;
  RAISE EXCEPTION 'Board command needs canonical source locks' USING ERRCODE='40001',CONSTRAINT='board_requires_payroll_source';
 END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('guard_payroll_protocol_statement()'::regprocedure),E'\r\n',E'\n');
 EXECUTE replace(definition,E'BEGIN\n',prefix);
END $$;
CREATE FUNCTION guard_board_operational_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('mserp.payroll.board_operational',true)='on' THEN
  IF TG_OP<>'INSERT' THEN
   IF OLD.entity_kind<>'driver' OR OLD.track<>'dispatch' THEN
    RAISE EXCEPTION 'Board command needs canonical source locks' USING ERRCODE='40001',CONSTRAINT='board_requires_payroll_source';
   END IF;
  END IF;
  IF TG_OP<>'DELETE' THEN
   IF NEW.entity_kind<>'driver' OR NEW.track<>'dispatch' THEN
    RAISE EXCEPTION 'Board command needs canonical source locks' USING ERRCODE='40001',CONSTRAINT='board_requires_payroll_source';
   END IF;
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER board_operational_history BEFORE INSERT OR UPDATE OR DELETE ON fleet_status_history
 FOR EACH ROW EXECUTE FUNCTION guard_board_operational_history();

-- Dispatch history has no payroll meaning. Availability/employment/condition
-- history remains captured, including mixed statements and deletes.
-- Capture and cache invalidation inspect both transition tables, so a mixed
-- statement or a change from a financial track to dispatch cannot disappear.
DO $$ DECLARE definition text; prefix text; BEGIN
 prefix:=$prefix$BEGIN
 IF TG_TABLE_NAME='fleet_status_history' AND TG_OP<>'TRUNCATE' THEN
  DECLARE relevant boolean=false;
  BEGIN
   IF TG_OP IN ('UPDATE','DELETE') THEN
    SELECT EXISTS(SELECT 1 FROM previous_rows WHERE entity_kind<>'driver' OR track<>'dispatch') INTO relevant;
   END IF;
   IF TG_OP IN ('INSERT','UPDATE') AND NOT relevant THEN
    SELECT EXISTS(SELECT 1 FROM changed_rows WHERE entity_kind<>'driver' OR track<>'dispatch') INTO relevant;
   END IF;
   IF NOT relevant THEN RETURN NULL; END IF;
  END;
 END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('capture_payroll_projection_change()'::regprocedure),E'\r\n',E'\n');
 EXECUTE replace(definition,E'BEGIN\n',prefix);
END $$;

CREATE FUNCTION invalidate_payroll_status_history_cache() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE relevant boolean=false;
BEGIN
 IF TG_OP='TRUNCATE' THEN relevant:=true; END IF;
 IF TG_OP IN ('UPDATE','DELETE') THEN
  SELECT EXISTS(SELECT 1 FROM previous_rows WHERE entity_kind<>'driver' OR track<>'dispatch') INTO relevant;
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') AND NOT relevant THEN
  SELECT EXISTS(SELECT 1 FROM changed_rows WHERE entity_kind<>'driver' OR track<>'dispatch') INTO relevant;
 END IF;
 IF relevant THEN UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='fleet_status_history'; END IF;
 RETURN NULL;
END $$;
DO $$ DECLARE op text; clause text; BEGIN
 FOREACH op IN ARRAY ARRAY['insert','update','delete','truncate'] LOOP
  EXECUTE format('DROP TRIGGER payroll_cache_%s ON fleet_status_history',op);
  clause:=CASE op WHEN 'insert' THEN 'REFERENCING NEW TABLE AS changed_rows'
   WHEN 'update' THEN 'REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows'
   WHEN 'delete' THEN 'REFERENCING OLD TABLE AS previous_rows' ELSE '' END;
  EXECUTE format('CREATE TRIGGER payroll_cache_%s AFTER %s ON fleet_status_history %s FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_status_history_cache()',op,upper(op),clause);
 END LOOP;
END $$;

-- Allow dispatchers to finish choosing pickup/delivery before a paid route call.
ALTER TABLE driver_board_automation ADD COLUMN route_not_before timestamptz;
CREATE FUNCTION defer_board_route() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='driver_board' THEN
  IF TG_OP='UPDATE' AND (NEW.current_load,NEW.status,NEW.destination) IS NOT DISTINCT FROM (OLD.current_load,OLD.status,OLD.destination) THEN RETURN NULL; END IF;
 ELSE
  IF TG_OP='UPDATE' AND (NEW.payload->'current',NEW.payload->'stopKey',NEW.payload->'destinationSource',NEW.payload->'destinationManual') IS NOT DISTINCT FROM
   (OLD.payload->'current',OLD.payload->'stopKey',OLD.payload->'destinationSource',OLD.payload->'destinationManual') THEN RETURN NULL; END IF;
 END IF;
 INSERT INTO driver_board_automation(driver_id,route_not_before) VALUES(NEW.driver_id,clock_timestamp()+interval '5 seconds')
 ON CONFLICT(driver_id) DO UPDATE SET route_not_before=EXCLUDED.route_not_before;
 RETURN NULL;
END $$;
CREATE TRIGGER defer_board_route AFTER INSERT OR UPDATE ON driver_board FOR EACH ROW EXECUTE FUNCTION defer_board_route();
CREATE TRIGGER defer_board_route AFTER INSERT OR UPDATE ON driver_board_load_state FOR EACH ROW EXECUTE FUNCTION defer_board_route();

-- Clearing a load/status must not hide Home/Vacation or a truck restriction.
DO $$ DECLARE definition text; BEGIN
 definition:=replace(pg_get_functiondef('fleet_status_board_before()'::regprocedure),E'\r\n',E'\n');
 IF strpos(definition,'IF NEW.status='''' THEN RETURN NEW; END IF;')=0 THEN RAISE EXCEPTION 'Unexpected board status guard'; END IF;
 definition:=replace(definition,'IF NEW.status='''' THEN RETURN NEW; END IF;',
  E'IF NEW.status='''' THEN\n  SELECT fleet_status_personal_label(availability_status) INTO NEW.status FROM drivers WHERE id=NEW.driver_id;\n  IF coalesce(NEW.status,'''')='''' THEN\n   NEW.status:=CASE fleet_truck_operating_status(fleet_status_driver_truck(NEW.driver_id)) WHEN ''shop'' THEN ''SHOP'' WHEN ''out_of_service'' THEN ''STOP'' WHEN ''terminated'' THEN ''STOP'' ELSE '''' END;\n  END IF;\n  RETURN NEW;\n END IF;');
 EXECUTE definition;
END $$;
-- Repair only the derived board label from the existing canonical profiles.
SELECT fleet_status_lock();
SELECT fleet_status_project_driver(id) FROM drivers WHERE active ORDER BY id;
UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,
 impact_roots=NULL,lease_token=NULL,lease_until=NULL,requested_at=clock_timestamp(),
 available_at=clock_timestamp(),attempts=0,last_error='';
COMMIT;
