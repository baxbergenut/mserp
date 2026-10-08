BEGIN;

-- Preserve source labels for display, but bind each imported load to a stable
-- local truck identity once. A reused/ambiguous label is never guessed.
CREATE TABLE truck_unit_aliases (
    unit_key text NOT NULL CHECK (unit_key <> '' AND unit_key=upper(btrim(unit_key))),
    truck_id uuid NOT NULL REFERENCES trucks(id) ON DELETE CASCADE,
    PRIMARY KEY(unit_key, truck_id)
);
CREATE INDEX truck_unit_aliases_truck_idx ON truck_unit_aliases(truck_id);
INSERT INTO truck_unit_aliases(unit_key,truck_id)
SELECT upper(btrim(unit_number)),id FROM trucks WHERE btrim(unit_number)<>''
UNION
SELECT upper(btrim(h.truck_unit)),h.truck_id FROM truck_ownership_history h
JOIN trucks t ON t.id=h.truck_id WHERE btrim(h.truck_unit)<>'';

ALTER TABLE loads ADD COLUMN truck_id uuid REFERENCES trucks(id) ON DELETE SET NULL;
CREATE INDEX loads_truck_id_idx ON loads(truck_id);
CREATE INDEX loads_unlinked_truck_label_idx ON loads(upper(btrim(truck_unit))) WHERE truck_id IS NULL;

CREATE FUNCTION resolve_load_truck(unit_label text) RETURNS uuid LANGUAGE sql STABLE AS $$
    SELECT CASE WHEN count(*)=1 THEN (array_agg(truck_id))[1] END
    FROM truck_unit_aliases WHERE unit_key=upper(btrim(unit_label))
$$;

UPDATE loads l SET truck_id=m.truck_id FROM (
    SELECT unit_key,(array_agg(truck_id))[1] truck_id FROM truck_unit_aliases
    GROUP BY unit_key HAVING count(*)=1
) m WHERE upper(btrim(l.truck_unit))=m.unit_key;

CREATE FUNCTION bind_load_truck() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.truck_id IS NOT NULL
       AND upper(btrim(NEW.truck_unit)) IS NOT DISTINCT FROM upper(btrim(OLD.truck_unit)) THEN
        NEW.truck_id := OLD.truck_id;
    ELSE
        NEW.truck_id := resolve_load_truck(NEW.truck_unit);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER loads_bind_truck BEFORE INSERT OR UPDATE OF truck_unit ON loads
FOR EACH ROW EXECUTE FUNCTION bind_load_truck();

CREATE FUNCTION record_truck_unit_alias() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES(upper(btrim(NEW.unit_number)),NEW.id)
    ON CONFLICT DO NOTHING;
    IF TG_OP='UPDATE' THEN
        INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES(upper(btrim(OLD.unit_number)),OLD.id)
        ON CONFLICT DO NOTHING;
    END IF;
    -- A truck created after its source loads can resolve only previously
    -- unlinked records. Existing identities survive later label reuse.
    UPDATE loads SET truck_id=NEW.id
    WHERE truck_id IS NULL AND upper(btrim(truck_unit))=upper(btrim(NEW.unit_number))
      AND resolve_load_truck(truck_unit)=NEW.id;
    RETURN NEW;
END $$;
CREATE TRIGGER trucks_unit_alias AFTER INSERT OR UPDATE OF unit_number ON trucks
FOR EACH ROW EXECUTE FUNCTION record_truck_unit_alias();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE truck_unit_aliases OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
