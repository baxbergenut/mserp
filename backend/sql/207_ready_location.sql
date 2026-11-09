BEGIN;
ALTER TABLE driver_board_automation ADD COLUMN ready_location jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE driver_board_automation ADD COLUMN ready_location_requested_at timestamptz;
ALTER TABLE board_route_usage DROP CONSTRAINT board_route_usage_provider_check;
ALTER TABLE board_route_usage ADD CONSTRAINT board_route_usage_provider_check CHECK (provider IN ('tomtom','google','tomtom_reverse'));

-- Commit-time notification: status edits are saved even when a provider is down.
-- The durable timestamp fences late results and survives listener restarts.
CREATE FUNCTION request_board_ready_location() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='READY' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status OR
   (btrim(NEW.destination)='' AND btrim(OLD.destination)<>'')) THEN
  INSERT INTO driver_board_automation(driver_id,ready_location_requested_at)
  VALUES(NEW.driver_id,clock_timestamp()) ON CONFLICT(driver_id) DO UPDATE
  SET ready_location_requested_at=EXCLUDED.ready_location_requested_at;
  PERFORM pg_notify('board_ready_location',NEW.driver_id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER request_board_ready_location AFTER INSERT OR UPDATE OF status,destination ON driver_board
FOR EACH ROW EXECUTE FUNCTION request_board_ready_location();
COMMIT;
