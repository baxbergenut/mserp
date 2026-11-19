BEGIN;

SELECT pg_advisory_xact_lock(736281940);
LOCK TABLE accounting_cutover, accounting_cutover_previews, accounting_payroll_archives,
 accounting_legacy_sources, accounting_rate_openings, accounting_rate_corrections,
 gross_board_transfer_history IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
 retired text[] := ARRAY['accounting_cutover','accounting_cutover_previews','accounting_payroll_archives',
  'accounting_legacy_sources','accounting_rate_openings','accounting_rate_corrections','gross_board_transfer_history'];
 relation_name text;
 populated boolean;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM accounting_cutover WHERE id AND version=1 AND NOT active
    AND week_start=DATE '2026-10-26' AND draft='{}'::jsonb AND activated_at IS NULL
    AND activated_by IS NULL AND receipt IS NULL) THEN
  RAISE EXCEPTION 'Accounting cutover singleton differs from the reviewed initial state';
 END IF;
 FOREACH relation_name IN ARRAY retired[2:7] LOOP
  EXECUTE format('SELECT EXISTS (SELECT FROM %I)',relation_name) INTO populated;
  IF populated THEN RAISE EXCEPTION 'Review retained records in %',relation_name; END IF;
 END LOOP;
 IF EXISTS (SELECT FROM payroll_projection_changes WHERE table_name=ANY(retired) AND dispatched_at IS NULL) THEN
  RAISE EXCEPTION 'Drain pending payroll captures before retiring tables';
 END IF;
 -- Keep the source identities referenced by historical captures and revision evidence.
 UPDATE payroll_projection_sources SET classification='unrelated' WHERE table_name=ANY(retired);
END $$;

DROP TABLE accounting_cutover_previews, accounting_payroll_archives, accounting_legacy_sources,
 accounting_rate_openings, accounting_rate_corrections, accounting_cutover, gross_board_transfer_history RESTRICT;
DROP FUNCTION protect_accounting_cutover(), protect_accounting_history(), protect_sheets_managed_plan(),
 discard_gross_transfer_history() RESTRICT;

COMMIT;
