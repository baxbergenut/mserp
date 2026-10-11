BEGIN;
CREATE FUNCTION prepass_truck_key(tag text,unit text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN btrim(coalesce(tag,''))<>'' THEN 'tag:'||upper(btrim(tag))
 WHEN btrim(coalesce(unit,''))<>'' AND upper(btrim(unit)) NOT LIKE 'UNASSIGNED%' AND upper(btrim(unit))<>'UNKNOWN'
 THEN 'unit:'||upper(btrim(unit)) ELSE '' END
$$;
CREATE TABLE prepass_truck_mappings (
 id uuid PRIMARY KEY,
 environment text NOT NULL CHECK(environment IN ('production','nonproduction')),
 identity_key text NOT NULL CHECK(identity_key<>''),
 truck_id uuid NOT NULL REFERENCES trucks(id) ON DELETE RESTRICT,
 effective_from date NOT NULL,
 effective_to date CHECK(effective_to>=effective_from),
 active boolean NOT NULL DEFAULT true,
 version integer NOT NULL DEFAULT 1,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prepass_truck_mappings_identity_idx ON prepass_truck_mappings(environment,identity_key) WHERE active;
ALTER TABLE tolls ADD COLUMN truck_mapping_id uuid REFERENCES prepass_truck_mappings(id) ON DELETE RESTRICT;
CREATE TABLE prepass_truck_mapping_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 mapping_id uuid NOT NULL REFERENCES prepass_truck_mappings(id),
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 action text NOT NULL CHECK(action IN ('link','undo')),
 transaction_ids jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
-- Confirmed mappings take precedence over unit-name guesses for new/unlinked rows.
-- Existing linked rows are retained on sync. Undo deactivates the mapping first.
CREATE FUNCTION apply_prepass_truck_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE matched prepass_truck_mappings%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.truck_id IS NOT NULL THEN RETURN NEW; END IF;
 END IF;
 SELECT * INTO matched FROM prepass_truck_mappings m
 WHERE m.active AND m.environment=coalesce(NEW.prepass_environment,'production')
 AND m.identity_key=prepass_truck_key(NEW.prepass_tag_id,NEW.equipment_unit)
 AND NEW.exit_date>=m.effective_from AND (m.effective_to IS NULL OR NEW.exit_date<=m.effective_to);
 IF FOUND THEN NEW.truck_id=matched.truck_id; NEW.truck_mapping_id=matched.id; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER toll_prepass_truck_mapping BEFORE INSERT OR UPDATE OF truck_id,equipment_unit,prepass_tag_id,exit_date ON tolls
 FOR EACH ROW EXECUTE FUNCTION apply_prepass_truck_mapping();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE prepass_truck_mappings OWNER TO mserp_app;
  ALTER TABLE prepass_truck_mapping_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
