-- Provider identity/verification audit records are not payroll inputs. The
-- resulting fuel attribution and expense rows remain captured financial sources.
-- Install with writers stopped and the compatible deploy helper in place.
BEGIN;

UPDATE payroll_projection_sources SET classification='unrelated'
WHERE table_name IN ('relay_driver_links','relay_identity_reviews','expense_verifications');

DO $$ DECLARE t text; trigger_name text; BEGIN
 FOREACH t IN ARRAY ARRAY['relay_driver_links','relay_identity_reviews','expense_verifications'] LOOP
  FOREACH trigger_name IN ARRAY ARRAY['payroll_protocol_statement','payroll_protocol_row',
   'payroll_projection_insert','payroll_projection_update','payroll_projection_delete','payroll_projection_truncate'] LOOP
   EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I',trigger_name,t);
  END LOOP;
 END LOOP;
END $$;

UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,
    impact_roots=NULL, lease_token=NULL, lease_until=NULL,
    requested_at=clock_timestamp(), available_at=clock_timestamp(),
    attempts=0, last_error='';

COMMIT;
