BEGIN;
ALTER TABLE prepass_fleet_operations DROP CONSTRAINT prepass_fleet_operations_state_check;
ALTER TABLE prepass_fleet_operations ADD CONSTRAINT prepass_fleet_operations_state_check
 CHECK(state IN ('pending','queued','confirmed','failed','verified'));
-- Accepted assignments wait independently; uncertain writes retain the global fence.
CREATE UNIQUE INDEX prepass_fleet_queued_vehicle ON prepass_fleet_operations(environment,account_number,vin) WHERE state='queued';
CREATE UNIQUE INDEX prepass_fleet_queued_device ON prepass_fleet_operations(environment,(request->>'transponderNumber')) WHERE state='queued';
COMMIT;
