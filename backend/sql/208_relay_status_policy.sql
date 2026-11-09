BEGIN;
ALTER TABLE relay_driver_rosters ADD COLUMN status_scan_cursor text NOT NULL DEFAULT '';
ALTER TABLE relay_driver_operations DROP CONSTRAINT relay_driver_operations_source_check;
ALTER TABLE relay_driver_operations ADD CONSTRAINT relay_driver_operations_source_check CHECK(source IN ('manual','termination','fleet_status'));
ALTER TABLE relay_driver_operations ADD COLUMN status_context jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE relay_driver_operations ADD COLUMN status_owned_revision text NOT NULL DEFAULT '';
ALTER TABLE relay_driver_operations ADD COLUMN status_ownership_revoked boolean NOT NULL DEFAULT false;

-- Employment changes permanently release ownership, including terminate/rehire
-- cycles that finish between worker passes. Never restore a former employee.
CREATE FUNCTION revoke_relay_status_employment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.status='terminated',OLD.active,OLD.termination_date) IS DISTINCT FROM (NEW.status='terminated',NEW.active,NEW.termination_date) THEN
  UPDATE relay_driver_operations SET status_ownership_revoked=true
  WHERE driver_id=NEW.id AND source='fleet_status' AND NOT status_ownership_revoked;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER revoke_relay_status_employment AFTER UPDATE OF status,active,termination_date ON drivers
FOR EACH ROW EXECUTE FUNCTION revoke_relay_status_employment();
CREATE FUNCTION revoke_relay_status_link() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE relay_driver_operations SET status_ownership_revoked=true
 WHERE environment=OLD.relay_environment AND relay_driver_id=OLD.relay_driver_id
 AND source='fleet_status' AND NOT status_ownership_revoked;
 RETURN OLD;
END $$;
CREATE TRIGGER revoke_relay_status_link AFTER UPDATE OF driver_id OR DELETE ON relay_driver_links
FOR EACH ROW EXECUTE FUNCTION revoke_relay_status_link();
COMMIT;
