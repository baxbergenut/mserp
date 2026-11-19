-- Current-week dispatch notes and trailer text do not change fleet state.
-- Full-row clients and changes to dispatch state retain fleet serialization.
DO $migration$
DECLARE
  guard regprocedure := to_regprocedure(format('%I.guard_team_dispatch()',current_schema()));
  definition text;
  old_guard text := 'IF TG_TABLE_NAME=''driver_board'' THEN PERFORM fleet_status_lock();';
  new_guard text := $guard$IF TG_TABLE_NAME='driver_board' THEN
    IF TG_OP='UPDATE' AND current_setting('mserp.board_text_edit',true)='on' THEN
      IF (to_jsonb(NEW) - ARRAY['notes','trailer_number','trailer_note','version','updated_at'])
          IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['notes','trailer_number','trailer_note','version','updated_at']) THEN
        RAISE EXCEPTION 'Unexpected dispatch change in board text edit' USING ERRCODE='23514';
      END IF;
      -- Concurrent text writers share this lock. A membership writer makes the
      -- application retry on the ordinary path after rolling back the text edit.
      LOCK TABLE driver_teams IN SHARE MODE NOWAIT;
    ELSE PERFORM fleet_status_lock(); END IF;$guard$;
BEGIN
  IF guard IS NULL THEN RETURN; END IF;
  definition := pg_get_functiondef(guard);
  IF strpos(definition,old_guard)=0 THEN RAISE EXCEPTION 'Unexpected team dispatch guard'; END IF;
  EXECUTE replace(definition,old_guard,new_guard);
  DROP TRIGGER fleet_status_lock ON driver_board;
  CREATE TRIGGER fleet_status_lock BEFORE INSERT OR DELETE ON driver_board
    FOR EACH STATEMENT EXECUTE FUNCTION fleet_status_statement_lock();
  CREATE TRIGGER fleet_status_lock_update BEFORE UPDATE ON driver_board
    FOR EACH STATEMENT WHEN (current_setting('mserp.board_text_edit',true) IS DISTINCT FROM 'on')
    EXECUTE FUNCTION fleet_status_statement_lock();
END $migration$;
