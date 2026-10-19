BEGIN;

-- Versioned records use JSON for category-specific descriptive fields; identity,
-- relationships and stock are constrained independently of those descriptions.
CREATE TABLE inventory_locations (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX inventory_location_name ON inventory_locations(lower(data->>'name'));
INSERT INTO inventory_locations(id,data) VALUES('019a0000-0000-4000-8000-000000000001','{"name":"Main storage","active":true}');

CREATE TABLE inventory_assets (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 truck_id uuid GENERATED ALWAYS AS (nullif(data->>'truckId','')::uuid) STORED REFERENCES trucks(id),
 location_id uuid GENERATED ALWAYS AS (nullif(data->>'locationId','')::uuid) STORED REFERENCES inventory_locations(id),
 gateway_id uuid GENERATED ALWAYS AS (nullif(data->>'gatewayId','')::uuid) STORED REFERENCES inventory_assets(id),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(gateway_id IS DISTINCT FROM id),
 CHECK(num_nonnulls(truck_id, location_id, nullif(data->>'custodian',''),gateway_id)<=1)
);
CREATE UNIQUE INDEX inventory_asset_imei ON inventory_assets((data->>'imei')) WHERE coalesce(data->>'imei','')<>'';
CREATE UNIQUE INDEX inventory_asset_serial ON inventory_assets(lower(data->>'serial')) WHERE coalesce(data->>'serial','')<>'';
CREATE UNIQUE INDEX inventory_asset_label ON inventory_assets(lower(data->>'assetLabel')) WHERE coalesce(data->>'assetLabel','')<>'';
CREATE UNIQUE INDEX inventory_truck_tablet ON inventory_assets(truck_id) WHERE data->>'deviceType'='tablet' AND coalesce((data->>'active')::boolean,true);
CREATE UNIQUE INDEX inventory_truck_gateway ON inventory_assets(truck_id) WHERE data->>'deviceType'='samsara_gateway' AND coalesce((data->>'active')::boolean,true);
CREATE UNIQUE INDEX inventory_gateway_camera ON inventory_assets(gateway_id) WHERE data->>'deviceType'='samsara_camera' AND coalesce((data->>'active')::boolean,true);
CREATE INDEX inventory_asset_truck ON inventory_assets(truck_id);

CREATE TABLE inventory_lines (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 asset_id uuid GENERATED ALWAYS AS (nullif(data->>'assetId','')::uuid) STORED REFERENCES inventory_assets(id),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX inventory_line_number ON inventory_lines((data->>'wirelessNumber'));
CREATE UNIQUE INDEX inventory_line_device ON inventory_lines(asset_id) WHERE asset_id IS NOT NULL AND coalesce((data->>'active')::boolean,true);

CREATE TABLE inventory_supplies (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX inventory_supply_identity ON inventory_supplies(lower(data->>'name'),coalesce(data->>'batchYear',''));
INSERT INTO inventory_supplies(id,data) VALUES
 ('019a0000-0000-4000-8000-000000000011','{"name":"Chains","unit":"sets","reusable":true,"active":true}'),
 ('019a0000-0000-4000-8000-000000000012','{"name":"Extinguisher","unit":"units","reusable":true,"active":true}'),
 ('019a0000-0000-4000-8000-000000000013','{"name":"MC Sticker","unit":"units","reusable":false,"active":true}'),
 ('019a0000-0000-4000-8000-000000000014','{"name":"IFTA Sticker","unit":"pairs","batchYear":"2026","reusable":false,"active":true}');

CREATE TABLE inventory_equipment (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 truck_id uuid GENERATED ALWAYS AS (nullif(data->>'truckId','')::uuid) STORED NOT NULL REFERENCES trucks(id),
 supply_id uuid GENERATED ALWAYS AS (nullif(data->>'supplyId','')::uuid) STORED REFERENCES inventory_supplies(id),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX inventory_equipment_identity ON inventory_equipment(truck_id,lower(data->>'name'),coalesce(data->>'batchYear',''));

CREATE TABLE fleet_trailers (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX fleet_trailer_unit ON fleet_trailers(lower(data->>'unitNumber'));
CREATE UNIQUE INDEX fleet_trailer_vin ON fleet_trailers(upper(data->>'vin')) WHERE coalesce(data->>'vin','')<>'';

-- Local annotations never create or overwrite a provider transponder roster.
CREATE TABLE inventory_transponder_notes (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX inventory_transponder_identity ON inventory_transponder_notes((data->>'providerEnvironment'),(data->>'providerAccount'),(data->>'serial'));

CREATE TABLE inventory_events (
 id uuid PRIMARY KEY, request_hash text NOT NULL, collection text NOT NULL, record_id uuid NOT NULL,
 action text NOT NULL, before_data jsonb, after_data jsonb NOT NULL,
 actor_id uuid, actor_name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX inventory_event_record ON inventory_events(record_id,created_at DESC);

CREATE TABLE inventory_stock_movements (
 id uuid PRIMARY KEY, request_hash text NOT NULL, supply_id uuid NOT NULL REFERENCES inventory_supplies(id),
 kind text NOT NULL CHECK(kind IN ('opening','receipt','issue','return','transfer','use','lost','damaged','adjustment','reversal')),
 from_holder text NOT NULL, to_holder text NOT NULL, quantity bigint NOT NULL CHECK(quantity>=0 AND quantity<=1000000),
 reason text NOT NULL CHECK(length(btrim(reason))>0), physical_count boolean NOT NULL DEFAULT false,
 reverses_id uuid UNIQUE REFERENCES inventory_stock_movements(id),
 actor_id uuid, actor_name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), CHECK(from_holder<>to_holder),
 CHECK((kind='reversal')=(reverses_id IS NOT NULL)), CHECK(quantity>0 OR (physical_count AND kind IN ('opening','adjustment')))
);
CREATE INDEX inventory_stock_supply ON inventory_stock_movements(supply_id,created_at DESC);

CREATE TABLE inventory_imports (
 id uuid PRIMARY KEY, version bigint NOT NULL DEFAULT 1, snapshot jsonb NOT NULL,
 plan jsonb NOT NULL, status text NOT NULL CHECK(status IN ('preview','committed')),
 source_hash text NOT NULL UNIQUE, created_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(), committed_at timestamptz
);

CREATE FUNCTION inventory_immutable_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'Inventory history is append-only; record a correction instead';
END $$;
CREATE TRIGGER inventory_events_immutable BEFORE UPDATE OR DELETE ON inventory_events FOR EACH ROW EXECUTE FUNCTION inventory_immutable_history();
CREATE TRIGGER inventory_stock_immutable BEFORE UPDATE OR DELETE ON inventory_stock_movements FOR EACH ROW EXECUTE FUNCTION inventory_immutable_history();

DO $$ DECLARE table_name text; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  FOREACH table_name IN ARRAY ARRAY['inventory_locations','inventory_assets','inventory_lines','inventory_supplies','inventory_equipment','fleet_trailers','inventory_transponder_notes','inventory_events','inventory_stock_movements','inventory_imports'] LOOP
   EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',table_name);
  END LOOP;
  ALTER FUNCTION inventory_immutable_history() OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
