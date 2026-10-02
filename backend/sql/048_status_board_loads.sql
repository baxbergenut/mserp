BEGIN;

-- A plan identity survives rate/date edits, but never follows a reused load slot.
ALTER TABLE gross_board_entries ADD COLUMN plan_id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE gross_board_extra_entries ADD COLUMN plan_id UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX gross_board_plan_id_idx ON gross_board_entries(plan_id);
CREATE UNIQUE INDEX gross_board_extra_plan_id_idx ON gross_board_extra_entries(plan_id);
CREATE FUNCTION version_gross_board_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF lower(btrim(NEW.load_number)) IS DISTINCT FROM lower(btrim(OLD.load_number))
       OR NEW.day_status IS DISTINCT FROM OLD.day_status
       OR (OLD.load_record_id IS NOT NULL AND NEW.load_record_id IS NOT NULL AND OLD.load_record_id<>NEW.load_record_id)
       OR (TG_TABLE_NAME='gross_board_extra_entries' AND (to_jsonb(NEW)->'deleted') IS DISTINCT FROM (to_jsonb(OLD)->'deleted')) THEN
        NEW.plan_id := gen_random_uuid();
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER gross_board_plan_identity BEFORE UPDATE ON gross_board_entries FOR EACH ROW EXECUTE FUNCTION version_gross_board_plan();
CREATE TRIGGER gross_board_extra_plan_identity BEFORE UPDATE ON gross_board_extra_entries FOR EACH ROW EXECUTE FUNCTION version_gross_board_plan();

-- Operational order/selection is independent of financial board placement.
CREATE TABLE driver_board_load_state (
    driver_id UUID PRIMARY KEY REFERENCES drivers(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(payload)='object')
);
CREATE FUNCTION audit_driver_board_loads() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous TEXT := '{}'; actor TEXT; actor_name_snapshot TEXT; driver_name_snapshot TEXT;
BEGIN
    IF TG_OP='UPDATE' THEN previous := OLD.payload::text; END IF;
    IF previous=NEW.payload::text THEN RETURN NEW; END IF;
    SELECT full_name INTO driver_name_snapshot FROM drivers WHERE id=NEW.driver_id;
    actor := coalesce(current_setting('mserp.board_actor',true),'');
    SELECT username INTO actor_name_snapshot FROM app_users WHERE id::text=actor;
    INSERT INTO driver_board_history(driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values)
    VALUES(NEW.driver_id,driver_name_snapshot,actor,coalesce(actor_name_snapshot,'System'),
      coalesce(nullif(current_setting('mserp.board_source',true),''),'database'),
      nullif(current_setting('mserp.board_undo',true),'')::bigint,
      jsonb_build_object('loadPlan',previous),jsonb_build_object('loadPlan',NEW.payload::text))
    ON CONFLICT(driver_id,transaction_id) DO UPDATE SET
      before_values=EXCLUDED.before_values || driver_board_history.before_values,
      after_values=driver_board_history.after_values || EXCLUDED.after_values;
    RETURN NEW;
END $$;
CREATE TRIGGER audit_driver_board_load_state AFTER INSERT OR UPDATE ON driver_board_load_state
FOR EACH ROW EXECUTE FUNCTION audit_driver_board_loads();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_board_load_state OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
