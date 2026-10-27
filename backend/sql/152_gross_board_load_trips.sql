-- Source trip selection is separate from the operational plan and driver pay.
-- Remember split evidence even if a later provider response omits its trips.
ALTER TABLE loads ADD COLUMN split_expected boolean NOT NULL DEFAULT false;
CREATE FUNCTION remember_load_split() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.split_expected := (TG_OP='UPDATE' AND OLD.split_expected) OR NEW.split_expected
  OR CASE WHEN jsonb_typeof(NEW.raw_payload->'trips')='array' THEN jsonb_array_length(NEW.raw_payload->'trips')>1 ELSE false END;
 RETURN NEW;
END $$;
CREATE TRIGGER remember_load_split BEFORE INSERT OR UPDATE ON loads FOR EACH ROW EXECUTE FUNCTION remember_load_split();
UPDATE loads SET split_expected=true WHERE CASE WHEN jsonb_typeof(raw_payload->'trips')='array' THEN jsonb_array_length(raw_payload->'trips')>1 ELSE false END;
ALTER TABLE gross_board_entries ADD COLUMN load_trip_id integer CHECK(load_trip_id>0);
ALTER TABLE gross_board_extra_entries ADD COLUMN load_trip_id integer CHECK(load_trip_id>0);
ALTER TABLE gross_board_entries ADD CONSTRAINT gross_trip_load CHECK(load_trip_id IS NULL OR (load_record_id IS NOT NULL AND day_status='' AND load_number<>''));
ALTER TABLE gross_board_extra_entries ADD CONSTRAINT gross_extra_trip_load CHECK(load_trip_id IS NULL OR (load_record_id IS NOT NULL AND day_status='' AND load_number<>'' AND NOT deleted));

CREATE TABLE gross_board_trip_history (
 id bigserial PRIMARY KEY, changed_at timestamptz NOT NULL DEFAULT now(),
 actor_id text NOT NULL, before_values jsonb, after_values jsonb NOT NULL
);
CREATE FUNCTION audit_gross_board_trip() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.load_trip_id IS NOT NULL OR (TG_OP='UPDATE' AND OLD.load_trip_id IS NOT NULL) THEN
  INSERT INTO gross_board_trip_history(actor_id,before_values,after_values)
  VALUES(coalesce(current_setting('mserp.board_actor',true),''),CASE WHEN TG_OP='UPDATE' THEN to_jsonb(OLD) END,to_jsonb(NEW));
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER gross_trip_history AFTER INSERT OR UPDATE ON gross_board_entries FOR EACH ROW EXECUTE FUNCTION audit_gross_board_trip();
CREATE TRIGGER gross_extra_trip_history AFTER INSERT OR UPDATE ON gross_board_extra_entries FOR EACH ROW EXECUTE FUNCTION audit_gross_board_trip();
CREATE TRIGGER protect_gross_trip_history BEFORE UPDATE OR DELETE ON gross_board_trip_history FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 ALTER TABLE gross_board_trip_history OWNER TO mserp_app;
 ALTER SEQUENCE gross_board_trip_history_id_seq OWNER TO mserp_app;
END IF; END $$;
