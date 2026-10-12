BEGIN;
ALTER TABLE prepass_truck_mappings ADD COLUMN account_number text;
ALTER TABLE prepass_truck_mappings ADD COLUMN fleet_vin text;
ALTER TABLE prepass_truck_mappings ADD COLUMN fleet_request_id uuid REFERENCES prepass_fleet_operations(id);
ALTER TABLE prepass_truck_mapping_events DROP CONSTRAINT prepass_truck_mapping_events_action_check;
ALTER TABLE prepass_truck_mapping_events ADD CONSTRAINT prepass_truck_mapping_events_action_check CHECK(action IN ('link','undo','close'));
CREATE INDEX prepass_truck_mappings_fleet_idx ON prepass_truck_mappings(environment,account_number,fleet_vin) WHERE active;

-- Legacy reviewed mappings retain their tag-first semantics. Fleet rules are
-- account scoped and also allow exact-unit fallback when no tag rule matches.
CREATE FUNCTION resolve_prepass_truck_mapping(env text, account text, tag text, unit text, travel date)
RETURNS SETOF prepass_truck_mappings LANGUAGE sql STABLE AS $$
 SELECT m.* FROM prepass_truck_mappings m
 WHERE m.active AND m.environment=env
 AND travel>=m.effective_from AND (m.effective_to IS NULL OR travel<=m.effective_to)
 AND ((m.account_number IS NULL AND m.identity_key=prepass_truck_key(tag,unit))
   OR (m.account_number=account AND m.identity_key IN
     (CASE WHEN btrim(coalesce(tag,''))<>'' THEN 'tag:'||upper(btrim(tag)) ELSE '' END,prepass_truck_key(NULL,unit))))
 ORDER BY (m.identity_key LIKE 'tag:%') DESC,(m.account_number IS NOT NULL) DESC,m.effective_from DESC,m.id
 LIMIT 1
$$;
CREATE OR REPLACE FUNCTION apply_prepass_truck_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE matched prepass_truck_mappings%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.truck_id IS NOT NULL THEN RETURN NEW; END IF;
 END IF;
 SELECT * INTO matched FROM resolve_prepass_truck_mapping(coalesce(NEW.prepass_environment,'production'),NEW.customer_id,NEW.prepass_tag_id,NEW.equipment_unit,NEW.exit_date);
 IF FOUND THEN NEW.truck_id=matched.truck_id; NEW.truck_mapping_id=matched.id; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER toll_prepass_truck_mapping ON tolls;
CREATE TRIGGER toll_prepass_truck_mapping BEFORE INSERT OR UPDATE OF truck_id,equipment_unit,prepass_tag_id,exit_date,customer_id,prepass_environment ON tolls
 FOR EACH ROW EXECUTE FUNCTION apply_prepass_truck_mapping();
COMMIT;
