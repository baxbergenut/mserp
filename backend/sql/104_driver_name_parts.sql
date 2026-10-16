BEGIN;

-- Existing full names are deliberately not split: token order does not prove
-- which words are given names or family names.
ALTER TABLE drivers ADD COLUMN first_name text;
ALTER TABLE drivers ADD COLUMN last_name text;
ALTER TABLE drivers ADD CONSTRAINT drivers_name_parts CHECK (
 (first_name IS NULL AND last_name IS NULL) OR
 (first_name IS NOT NULL AND last_name IS NOT NULL
  AND char_length(first_name) BETWEEN 1 AND 50
  AND char_length(last_name) BETWEEN 1 AND 50)
);

CREATE FUNCTION driver_name_parts_canonical() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.first_name IS NOT NULL THEN
  NEW.first_name := btrim(regexp_replace(NEW.first_name, '[[:space:]]+', ' ', 'g'));
 END IF;
 IF NEW.last_name IS NOT NULL THEN
  NEW.last_name := btrim(regexp_replace(NEW.last_name, '[[:space:]]+', ' ', 'g'));
 END IF;
 IF NEW.first_name IS NOT NULL AND NEW.last_name IS NOT NULL THEN
  -- Reviewed parts are authoritative even for older full-name-only writers.
  NEW.full_name := NEW.first_name || ' ' || NEW.last_name;
  NEW.normalized_name := lower(NEW.full_name);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_name_parts_canonical BEFORE INSERT OR UPDATE ON drivers
 FOR EACH ROW EXECUTE FUNCTION driver_name_parts_canonical();

COMMIT;
