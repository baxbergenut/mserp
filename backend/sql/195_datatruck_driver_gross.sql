BEGIN;
-- Existing values (including historical blanks) are left untouched. Only new
-- blank plans wait for an initial DataTruck amount. Clearing a saved amount is manual.
ALTER TABLE gross_board_entries ADD COLUMN driver_gross_pending boolean NOT NULL DEFAULT false;
ALTER TABLE gross_board_extra_entries ADD COLUMN driver_gross_pending boolean NOT NULL DEFAULT false;

CREATE FUNCTION track_pending_driver_gross() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.driver_rate IS NOT NULL OR NEW.day_status<>'' OR btrim(NEW.load_number)=''
     OR coalesce((to_jsonb(NEW)->>'deleted')::boolean,false) THEN
    NEW.driver_gross_pending := false;
  ELSIF TG_OP='INSERT' THEN
    NEW.driver_gross_pending := true;
  ELSIF OLD.load_number IS DISTINCT FROM NEW.load_number
     OR OLD.load_trip_id IS DISTINCT FROM NEW.load_trip_id THEN
    NEW.driver_gross_pending := true;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER track_pending_driver_gross BEFORE INSERT OR UPDATE ON gross_board_entries
FOR EACH ROW EXECUTE FUNCTION track_pending_driver_gross();
CREATE TRIGGER track_pending_driver_gross BEFORE INSERT OR UPDATE ON gross_board_extra_entries
FOR EACH ROW EXECUTE FUNCTION track_pending_driver_gross();

CREATE INDEX gross_board_pending_driver_gross_idx ON gross_board_entries(load_number) WHERE driver_gross_pending;
CREATE INDEX gross_board_extra_pending_driver_gross_idx ON gross_board_extra_entries(load_number) WHERE driver_gross_pending;
COMMIT;
