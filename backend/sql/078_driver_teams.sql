BEGIN;
ALTER TABLE drivers ADD COLUMN team_version integer NOT NULL DEFAULT 0;
CREATE TABLE driver_teams (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 main_driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 team_driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 start_week date NOT NULL CHECK(extract(isodow FROM start_week)=1),
 end_week date CHECK(extract(isodow FROM end_week)=1 AND end_week>=start_week),
 CHECK(main_driver_id<>team_driver_id)
);
CREATE INDEX driver_teams_main_idx ON driver_teams(main_driver_id,start_week);
CREATE INDEX driver_teams_member_idx ON driver_teams(team_driver_id,start_week);
CREATE TABLE driver_team_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 driver_id uuid NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 effective_week date NOT NULL,
 before_value jsonb NOT NULL,
 after_value jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION payroll_driver_at(person uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT coalesce((SELECT main_driver_id FROM driver_teams WHERE team_driver_id=person
 AND start_week<=day AND (end_week IS NULL OR day<end_week)),person)
$$;
-- Serialize configuration with payroll, fleet writes and other team changes.
CREATE FUNCTION guard_driver_team() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE first_week date; last_week date;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 first_week:=NEW.start_week;last_week:=NEW.end_week;
 IF TG_OP='UPDATE' THEN
  IF NEW.main_driver_id=OLD.main_driver_id AND NEW.team_driver_id=OLD.team_driver_id AND NEW.start_week=OLD.start_week THEN
   IF NEW.end_week IS NOT DISTINCT FROM OLD.end_week THEN RETURN NEW; END IF;
   first_week:=least(NEW.end_week,OLD.end_week);
   IF NEW.end_week IS NULL OR OLD.end_week IS NULL THEN last_week:=NULL; ELSE last_week:=greatest(NEW.end_week,OLD.end_week); END IF;
  ELSE first_week:=least(NEW.start_week,OLD.start_week);last_week:=NULL;
  END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id IN (NEW.main_driver_id,NEW.team_driver_id)
 AND s.week_start>=first_week AND (last_week IS NULL OR s.week_start<last_week) AND s.finalized) THEN
  RAISE EXCEPTION 'Reopen finalized payroll before changing this team' USING ERRCODE='23514';
 END IF;
 IF NEW.end_week=NEW.start_week THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM driver_teams t WHERE t.id<>NEW.id
 AND (t.main_driver_id IN (NEW.main_driver_id,NEW.team_driver_id) OR t.team_driver_id IN (NEW.main_driver_id,NEW.team_driver_id))
 AND daterange(t.start_week,t.end_week,'[)') && daterange(NEW.start_week,NEW.end_week,'[)')) THEN
  RAISE EXCEPTION 'A driver can belong to only one two-person team in a week' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_team_guard BEFORE INSERT OR UPDATE ON driver_teams FOR EACH ROW EXECUTE FUNCTION guard_driver_team();

-- Team members keep their personal ledgers, protected by the main statement.
CREATE FUNCTION protect_team_payroll() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; w date; value jsonb;
BEGIN
 IF TG_OP='DELETE' THEN value:=to_jsonb(OLD); ELSE value:=to_jsonb(NEW); END IF;
 w:=(value->>'week_start')::date;
 IF TG_TABLE_NAME IN ('driver_pay_weeks','driver_pay_cost_collections') THEN d:=(value->>'driver_id')::uuid;
 ELSIF TG_TABLE_NAME='expense_payments' THEN SELECT charge_driver_id INTO d FROM expenses WHERE id=(value->>'expense_id')::uuid;
 ELSIF TG_TABLE_NAME IN ('driver_escrow_payments','driver_escrow_releases') THEN SELECT driver_id INTO d FROM driver_escrows WHERE id=(value->>'escrow_id')::uuid;
 ELSE SELECT driver_id INTO d FROM driver_charge_schedules WHERE id=(value->>'schedule_id')::uuid;
 END IF;
 IF d IS NOT NULL AND payroll_driver_at(d,w)<>d AND EXISTS(SELECT 1 FROM payroll_settlements
 WHERE driver_id=payroll_driver_at(d,w) AND week_start=w AND finalized) THEN
  IF TG_OP='UPDATE' AND TG_TABLE_NAME='driver_charge_occurrences' AND
    (value - ARRAY['version'])=(to_jsonb(OLD) - ARRAY['version']) THEN RETURN NEW; END IF;
  RAISE EXCEPTION 'Reopen the main driver settlement before editing team deductions' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER team_payroll_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_weeks FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_cost_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_pay_cost_collections FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_expense_guard BEFORE INSERT OR UPDATE OR DELETE ON expense_payments FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_charge_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_escrow_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_payments FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();
CREATE TRIGGER team_release_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_releases FOR EACH ROW EXECUTE FUNCTION protect_team_payroll();

CREATE FUNCTION guard_team_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE day date;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(736281940);
 IF TG_TABLE_NAME='truck_driver_assignments' THEN
  IF EXISTS(SELECT 1 FROM driver_teams t WHERE t.team_driver_id=NEW.driver_id
    AND daterange(t.start_week,t.end_week,'[)') && daterange((NEW.assigned_at AT TIME ZONE 'America/New_York')::date,
      (NEW.unassigned_at AT TIME ZONE 'America/New_York')::date,'[)')) THEN
   RAISE EXCEPTION 'Assign the truck to the main driver of this team' USING ERRCODE='23514';
  END IF;
 ELSE
  IF TG_TABLE_NAME='driver_board' THEN day:=(now() AT TIME ZONE 'America/New_York')::date;
  ELSE
   day:=NEW.service_date;
   IF btrim(NEW.load_number)='' THEN RETURN NEW; END IF;
   IF TG_TABLE_NAME='gross_board_extra_entries' THEN
    IF NEW.deleted THEN RETURN NEW; END IF;
   END IF;
  END IF;
  IF payroll_driver_at(NEW.driver_id,day)<>NEW.driver_id THEN
   RAISE EXCEPTION 'Enter dispatch work under the main driver of this team' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER team_truck_guard BEFORE INSERT OR UPDATE ON truck_driver_assignments FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();
CREATE TRIGGER team_gross_guard BEFORE INSERT OR UPDATE ON gross_board_entries FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();
CREATE TRIGGER team_extra_gross_guard BEFORE INSERT OR UPDATE ON gross_board_extra_entries FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();
CREATE TRIGGER team_board_guard BEFORE INSERT OR UPDATE ON driver_board FOR EACH ROW EXECUTE FUNCTION guard_team_dispatch();

CREATE FUNCTION guard_team_recurring() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d uuid; k text; first_week date; last_week date;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(736281940);
 IF TG_TABLE_NAME='driver_charge_schedules' THEN
  IF TG_OP='UPDATE' AND NEW.driver_id=OLD.driver_id AND NEW.kind=OLD.kind AND NEW.start_week=OLD.start_week THEN RETURN NEW; END IF;
  d:=NEW.driver_id;k:=NEW.kind;first_week:=NEW.start_week;last_week:=NEW.end_week+7;
 ELSE
  IF NEW.paused THEN RETURN NEW; END IF;
  SELECT driver_id,kind,end_week+7 INTO d,k,last_week FROM driver_charge_schedules WHERE id=NEW.schedule_id;
  first_week:=NEW.week_start;
  SELECT least(last_week,min(week_start)) INTO last_week FROM driver_charge_phases WHERE schedule_id=NEW.schedule_id AND week_start>first_week;
 END IF;
 IF k='recurring' AND EXISTS(SELECT 1 FROM driver_teams t WHERE t.team_driver_id=d
 AND daterange(t.start_week,t.end_week,'[)') && daterange(first_week,last_week,'[)')) THEN
  RAISE EXCEPTION 'Configure recurring charges on the main driver, not the team driver' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER team_recurring_schedule_guard BEFORE INSERT OR UPDATE ON driver_charge_schedules FOR EACH ROW EXECUTE FUNCTION guard_team_recurring();
CREATE TRIGGER team_recurring_phase_guard BEFORE INSERT OR UPDATE ON driver_charge_phases FOR EACH ROW EXECUTE FUNCTION guard_team_recurring();

CREATE OR REPLACE FUNCTION assert_assignment_payroll_open(d uuid, t uuid, effective_week date)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE
 (s.driver_id=d OR s.driver_id=payroll_driver_at(d,s.week_start)) AND s.week_start>=effective_week AND s.finalized) THEN
  RAISE EXCEPTION 'Reopen finalized payroll for this driver from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=t AND week_start>=effective_week AND finalized) THEN
  RAISE EXCEPTION 'Reopen finalized investor payroll for this truck from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
 END IF;
END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_teams OWNER TO mserp_app;
  ALTER TABLE driver_team_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
