BEGIN;
ALTER TABLE relay_driver_rosters ADD COLUMN status_scan_cursor text NOT NULL DEFAULT '';
ALTER TABLE relay_driver_operations DROP CONSTRAINT relay_driver_operations_source_check;
ALTER TABLE relay_driver_operations ADD CONSTRAINT relay_driver_operations_source_check CHECK(source IN ('manual','termination','fleet_status'));
ALTER TABLE relay_driver_operations ADD COLUMN status_context jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMIT;
