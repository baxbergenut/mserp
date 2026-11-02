-- Inspection checklists and their layout/history never feed payroll amounts.
-- Apply with writers quiesced and the matching deploy helper installed.
BEGIN;
SELECT pg_advisory_xact_lock(736281941);

UPDATE payroll_projection_sources SET classification='unrelated'
WHERE table_name IN ('weekly_pti_weeks','weekly_pti_rows','weekly_pti_events',
 'weekly_pti_layout_state','weekly_pti_layout','weekly_pti_layout_events');

DO $$ DECLARE t text; trigger_name text; BEGIN
 FOREACH t IN ARRAY ARRAY['weekly_pti_weeks','weekly_pti_rows','weekly_pti_events',
 'weekly_pti_layout_state','weekly_pti_layout','weekly_pti_layout_events'] LOOP
  FOREACH trigger_name IN ARRAY ARRAY['payroll_protocol_statement','payroll_protocol_row',
   'payroll_projection_insert','payroll_projection_update','payroll_projection_delete','payroll_projection_truncate'] LOOP
   EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I',trigger_name,t);
  END LOOP;
 END LOOP;
END $$;

-- Ordinary cache revisions and immutable inspection audit history stay intact.
-- Publish retained weeks against the new fingerprint once, after activation.
UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,
 impact_roots=NULL,lease_token=NULL,lease_until=NULL,
 requested_at=clock_timestamp(),available_at=clock_timestamp(),attempts=0,last_error='';
COMMIT;
