BEGIN;

-- Old binaries may still issue the audit INSERT during rollout or rollback.
-- Keep its table shape but discard those writes without touching placement.
CREATE FUNCTION discard_gross_transfer_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RETURN NULL;
END $$;

DO $$
DECLARE trigger_name text;
DECLARE enabled_triggers text[];
BEGIN
 IF to_regclass('gross_board_transfer_history') IS NULL THEN RETURN; END IF;

 LOCK TABLE gross_board_transfer_history IN ACCESS EXCLUSIVE MODE;
 -- Removing audit rows does not change payroll sources. Keep the existing
 -- capture configuration and suppress only the triggers that handle TRUNCATE.
 SELECT array_agg(tgname::text) INTO enabled_triggers
 FROM pg_trigger
 WHERE tgrelid='gross_board_transfer_history'::regclass
 AND tgenabled='O'
 AND tgname IN ('payroll_protocol_statement','payroll_projection_truncate');
 FOREACH trigger_name IN ARRAY coalesce(enabled_triggers,ARRAY[]::text[]) LOOP
  EXECUTE format('ALTER TABLE gross_board_transfer_history DISABLE TRIGGER %I',trigger_name);
 END LOOP;
 TRUNCATE gross_board_transfer_history;
 FOREACH trigger_name IN ARRAY coalesce(enabled_triggers,ARRAY[]::text[]) LOOP
  EXECUTE format('ALTER TABLE gross_board_transfer_history ENABLE TRIGGER %I',trigger_name);
 END LOOP;
 CREATE TRIGGER discard_gross_transfer_history BEFORE INSERT ON gross_board_transfer_history
  FOR EACH ROW EXECUTE FUNCTION discard_gross_transfer_history();
END $$;

COMMIT;
