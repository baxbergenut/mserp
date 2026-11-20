-- A load command can cross a run of notes/trailer edits. All other writes
-- invalidate older commands even if the load later returns to its original value.
-- Existing rows start with their current version as the conservative boundary.
ALTER TABLE driver_board ADD COLUMN IF NOT EXISTS load_action_version integer;

CREATE OR REPLACE FUNCTION set_board_load_action_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND current_setting('mserp.board_text_edit',true)='on'
       AND NEW.version=OLD.version+1
       AND (to_jsonb(NEW) - ARRAY['notes','trailer_number','trailer_note','updated_at','version','load_action_version'])
           = (to_jsonb(OLD) - ARRAY['notes','trailer_number','trailer_note','updated_at','version','load_action_version']) THEN
        NEW.load_action_version := coalesce(OLD.load_action_version,OLD.version);
    ELSE
        NEW.load_action_version := NEW.version;
    END IF;
    RETURN NEW;
END $$;

-- Run after fleet normalization and the team guard has checked the text-only edit.
CREATE OR REPLACE TRIGGER zz_board_load_action_version BEFORE INSERT OR UPDATE ON driver_board
    FOR EACH ROW EXECUTE FUNCTION set_board_load_action_version();
