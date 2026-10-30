BEGIN;

CREATE TABLE status_color_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 colors jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(colors)='object'),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL
);
INSERT INTO status_color_settings(singleton) VALUES(true);
CREATE TABLE status_color_history (
 version integer PRIMARY KEY,
 colors jsonb NOT NULL,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 recorded_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION protect_status_color_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Status color history is append-only' USING ERRCODE='23514'; END $$;
CREATE TRIGGER status_color_history_immutable BEFORE UPDATE OR DELETE ON status_color_history FOR EACH ROW EXECUTE FUNCTION protect_status_color_history();
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 ALTER TABLE status_color_settings OWNER TO mserp_app;
 ALTER TABLE status_color_history OWNER TO mserp_app;
END IF; END $$;

-- Compatibility aliases remain readable in old receipts and frozen statements.
-- New live writes and undo recovery always use the current vocabulary.
CREATE FUNCTION normalize_live_board_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.status:=CASE NEW.status WHEN 'At PU' THEN 'DISPATCHED' WHEN 'At DEL' THEN 'ENROUTE'
 WHEN 'HOME (YARD)' THEN 'HOME' WHEN '34 RESET' THEN 'REST' WHEN 'RESET' THEN 'REST' ELSE NEW.status END;
 RETURN NEW;
END $$;
CREATE TRIGGER a_normalize_live_status BEFORE INSERT OR UPDATE OF status ON driver_board FOR EACH ROW EXECUTE FUNCTION normalize_live_board_status();
CREATE FUNCTION normalize_driver_rest() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.availability_status='reset' THEN NEW.availability_status:='rest'; END IF; RETURN NEW; END $$;
CREATE TRIGGER a_normalize_driver_rest BEFORE INSERT OR UPDATE OF availability_status ON drivers FOR EACH ROW EXECUTE FUNCTION normalize_driver_rest();
CREATE OR REPLACE FUNCTION fleet_status_personal_label(availability text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE availability WHEN 'home' THEN 'HOME' WHEN 'vacation' THEN 'VACATION' WHEN 'rest' THEN 'REST' WHEN 'reset' THEN 'REST'
 WHEN 'family_issue' THEN 'Family issue' WHEN 'health_issue' THEN 'Health issue' WHEN 'new_driver' THEN 'NEW DRIVER' ELSE '' END
$$;
DELETE FROM fleet_status_rules WHERE board_status IN ('At PU','At DEL','HOME (YARD)','34 RESET');
SELECT fleet_status_lock();
SELECT set_config('mserp.board_source','status_vocabulary_migration',true),
 set_config('mserp.status_reason','Consolidate retired status labels; preserve availability, loads and ETA',true),
 set_config('mserp.status_projection','1',true);
UPDATE drivers SET availability_status='rest',updated_at=now() WHERE availability_status='reset';
UPDATE driver_board SET status=CASE status WHEN 'At PU' THEN 'DISPATCHED' WHEN 'At DEL' THEN 'ENROUTE'
 WHEN 'HOME (YARD)' THEN 'HOME' ELSE 'REST' END,version=version+1,updated_at=now()
 WHERE status IN ('At PU','At DEL','HOME (YARD)','34 RESET','RESET');
SELECT set_config('mserp.status_projection','',true);

-- No historical gross rows or finalized financial snapshots are rewritten.
ALTER TABLE gross_board_entries DROP CONSTRAINT IF EXISTS gross_board_day_status_valid,
 DROP CONSTRAINT IF EXISTS gross_board_entries_day_status_check;
ALTER TABLE gross_board_entries ADD CONSTRAINT gross_board_day_status_valid CHECK
 (day_status IN ('','SHOP','HOME','REST','RESET','ENROUTE','IN TRANSIT','REJECTED','LOAD CANCELLED','NO LOAD','STUCK','LATE DEL','TRUCK ISSUE','LEFT','NEW DRIVER','DEADHEAD'));
ALTER TABLE gross_board_extra_entries DROP CONSTRAINT IF EXISTS gross_board_extra_entries_day_status_check;
ALTER TABLE gross_board_extra_entries ADD CONSTRAINT gross_board_extra_entries_day_status_check CHECK
 (day_status IN ('','SHOP','HOME','REST','RESET','ENROUTE','IN TRANSIT','REJECTED','LOAD CANCELLED','NO LOAD','STUCK','LATE DEL','TRUCK ISSUE','LEFT','NEW DRIVER','DEADHEAD'));
COMMIT;
