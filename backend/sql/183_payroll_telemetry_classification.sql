-- Vehicle telemetry is maintenance evidence, not an input to either pay page.
-- Apply with writers quiesced: old binaries do not know this exclusion.
BEGIN;

UPDATE payroll_projection_sources SET classification='unrelated'
WHERE table_name='maintenance_telemetry';

DROP TRIGGER IF EXISTS payroll_protocol_statement ON maintenance_telemetry;
DROP TRIGGER IF EXISTS payroll_protocol_row ON maintenance_telemetry;
DROP TRIGGER IF EXISTS payroll_projection_insert ON maintenance_telemetry;
DROP TRIGGER IF EXISTS payroll_projection_update ON maintenance_telemetry;
DROP TRIGGER IF EXISTS payroll_projection_delete ON maintenance_telemetry;
DROP TRIGGER IF EXISTS payroll_projection_truncate ON maintenance_telemetry;

-- Keep the ordinary cache revision triggers and all captured/history records.
-- Reconcile retained periods against the new source fingerprint before cutover.
UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,
    impact_roots=NULL, lease_token=NULL, lease_until=NULL,
    requested_at=clock_timestamp(), available_at=clock_timestamp(),
    attempts=0, last_error='';

COMMIT;
