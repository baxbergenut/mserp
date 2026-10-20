BEGIN;

-- Existing descriptions, custody and source observations require review. This
-- migration adds storage/guards only; it does not infer or rewrite fleet facts.
CREATE TABLE inventory_custodians (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX inventory_custodian_name ON inventory_custodians(lower(data->>'name'));
ALTER TABLE inventory_assets ADD COLUMN custodian_id uuid GENERATED ALWAYS AS (nullif(data->>'custodianId','')::uuid) STORED REFERENCES inventory_custodians(id);
ALTER TABLE inventory_assets ADD CONSTRAINT inventory_holder_exclusive CHECK(num_nonnulls(truck_id,location_id,gateway_id,custodian_id,nullif(data->>'custodian',''))<=1);
ALTER TABLE inventory_assets DROP CONSTRAINT inventory_device_status;
ALTER TABLE inventory_assets ADD CONSTRAINT inventory_device_status CHECK(coalesce(data->>'status','') IN ('','unknown','active','inactive','spare','retired','lost','sold','total_loss'));
ALTER TABLE inventory_assets ADD CONSTRAINT inventory_device_details CHECK(
 length(coalesce(data->>'color',''))<=50 AND length(coalesce(data->>'caseColor',''))<=50 AND length(coalesce(data->>'modelNumber',''))<=50 AND
 (coalesce(data->>'storageGb','')='' OR (data->>'storageGb' ~ '^[1-9][0-9]{0,4}$' AND (data->>'storageGb')::integer<=65536)));
CREATE INDEX inventory_asset_custodian ON inventory_assets(custodian_id);
CREATE INDEX inventory_asset_category ON inventory_assets((data->>'deviceType'),(data->>'active'));
-- Older clients cannot erase attributes they do not understand. Current writers
-- send explicit empty strings when clearing an optional attribute.
CREATE FUNCTION inventory_preserve_details() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE field text;
BEGIN
 FOREACH field IN ARRAY ARRAY['modelNumber','storageGb','color','caseColor','custodianId','originalModel'] LOOP
  IF OLD.data ? field AND NOT (NEW.data ? field) THEN NEW.data:=jsonb_set(NEW.data,ARRAY[field],OLD.data->field); END IF;
 END LOOP;
 IF coalesce(OLD.data->>'model','')<>coalesce(NEW.data->>'model','') AND coalesce(NEW.data->>'originalModel','')='' THEN
  NEW.data:=jsonb_set(NEW.data,'{originalModel}',to_jsonb(coalesce(OLD.data->>'model','')));
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER inventory_preserve_details BEFORE UPDATE ON inventory_assets FOR EACH ROW EXECUTE FUNCTION inventory_preserve_details();
ALTER TABLE inventory_events ADD COLUMN reason text NOT NULL DEFAULT '', ADD COLUMN effective_date date;
CREATE TABLE inventory_operations (
 id uuid PRIMARY KEY, request_hash text NOT NULL, result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER inventory_operations_immutable BEFORE UPDATE OR DELETE ON inventory_operations FOR EACH ROW EXECUTE FUNCTION inventory_immutable_history();

-- Legacy incompatible links stay visible for review. Future edits must resolve
-- them; deployment itself never releases a camera or changes a source status.
CREATE FUNCTION inventory_lifecycle_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM inventory_assets a
  WHERE (a.id=NEW.id OR a.gateway_id=NEW.id OR a.id=(SELECT gateway_id FROM inventory_assets WHERE id=NEW.id))
   AND (coalesce((a.data->>'active')::boolean,true)=false OR a.data->>'status' IN ('sold','total_loss','lost','retired') OR a.data->>'condition' IN ('lost','retired'))
   AND (a.truck_id IS NOT NULL OR a.gateway_id IS NOT NULL OR
    EXISTS(SELECT 1 FROM inventory_assets c WHERE c.gateway_id=a.id AND coalesce((c.data->>'active')::boolean,true)) OR
    EXISTS(SELECT 1 FROM inventory_lines l WHERE l.asset_id=a.id AND coalesce((l.data->>'active')::boolean,true)))
 ) THEN RAISE EXCEPTION 'Release assignments and linked devices/lines before marking a device unavailable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER inventory_lifecycle_guard AFTER INSERT OR UPDATE ON inventory_assets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION inventory_lifecycle_guard();
CREATE FUNCTION inventory_line_lifecycle_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM inventory_lines l JOIN inventory_assets a ON a.id=l.asset_id WHERE l.id=NEW.id AND coalesce((l.data->>'active')::boolean,true) AND
 (coalesce((a.data->>'active')::boolean,true)=false OR a.data->>'status' IN ('sold','total_loss','lost','retired') OR a.data->>'condition' IN ('lost','retired')))
 THEN RAISE EXCEPTION 'A service line requires an available device' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER inventory_line_lifecycle_guard AFTER INSERT OR UPDATE ON inventory_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION inventory_line_lifecycle_guard();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE inventory_custodians OWNER TO mserp_app;
  ALTER TABLE inventory_operations OWNER TO mserp_app;
  ALTER FUNCTION inventory_lifecycle_guard() OWNER TO mserp_app;
  ALTER FUNCTION inventory_line_lifecycle_guard() OWNER TO mserp_app;
  ALTER FUNCTION inventory_preserve_details() OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
