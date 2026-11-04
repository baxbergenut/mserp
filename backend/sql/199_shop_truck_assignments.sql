-- Shop is a dispatch restriction, not a restriction on recording a driver/crew.
-- Preserve the existing status, reasons and independent work-order restrictions.
BEGIN;
CREATE OR REPLACE FUNCTION fleet_truck_assignment_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.unassigned_at IS NULL AND (TG_OP='INSERT' OR OLD.unassigned_at IS NOT NULL OR NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.driver_id IS DISTINCT FROM OLD.driver_id) THEN
  PERFORM id FROM trucks WHERE id=NEW.truck_id FOR UPDATE;
  IF fleet_truck_operating_status(NEW.truck_id) NOT IN ('ready','in_use','shop') THEN RAISE EXCEPTION 'Return the truck to service before assigning a driver' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION fleet_truck_team_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE truck uuid;
BEGIN
 IF NEW.start_week<=(now() AT TIME ZONE 'America/New_York')::date AND (NEW.end_week IS NULL OR NEW.end_week>(now() AT TIME ZONE 'America/New_York')::date)
 AND (TG_OP='INSERT' OR NEW.main_driver_id IS DISTINCT FROM OLD.main_driver_id OR NEW.team_driver_id IS DISTINCT FROM OLD.team_driver_id OR NEW.start_week IS DISTINCT FROM OLD.start_week OR (OLD.end_week IS NOT NULL AND OLD.end_week<=(now() AT TIME ZONE 'America/New_York')::date)) THEN
  SELECT a.truck_id INTO truck FROM truck_driver_assignments a WHERE a.driver_id=NEW.main_driver_id AND a.unassigned_at IS NULL;
  PERFORM id FROM trucks WHERE id=truck FOR UPDATE;
  IF fleet_truck_operating_status(truck) NOT IN ('ready','in_use','shop') THEN RAISE EXCEPTION 'Return the truck to service before assigning a team driver' USING ERRCODE='23514',CONSTRAINT='fleet_status_invalid'; END IF;
 END IF;
 RETURN NEW;
END $$;

COMMIT;
