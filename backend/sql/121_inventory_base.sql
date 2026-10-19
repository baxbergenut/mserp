BEGIN;
SELECT pg_advisory_xact_lock(119,1);

-- Preserve the complete before/after record for each data upgrade. The user-facing
-- history/stock tools are deferred; the durable audit remains append-only.
CREATE OR REPLACE FUNCTION pg_temp.inventory_base_update(target_table text, category text, record_id uuid, new_data jsonb)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE old_record record; new_record record; BEGIN
 EXECUTE format('SELECT * FROM %I WHERE id=$1 FOR UPDATE',target_table) INTO old_record USING record_id;
 IF old_record.data IS NOT DISTINCT FROM new_data THEN RETURN; END IF;
 EXECUTE format('UPDATE %I SET data=$2,version=version+1,updated_by=NULL,updated_at=now() WHERE id=$1 RETURNING *',target_table) INTO new_record USING record_id,new_data;
 INSERT INTO inventory_events(id,request_hash,collection,record_id,action,before_data,after_data,actor_name)
 VALUES(gen_random_uuid(),md5('inventory-base:'||record_id::text||old_record.version)||md5(new_data::text),category,record_id,'normalize',
 jsonb_build_object('id',old_record.id,'version',old_record.version,'data',old_record.data,'updatedAt',old_record.updated_at),
 jsonb_build_object('id',new_record.id,'version',new_record.version,'data',new_record.data,'updatedAt',new_record.updated_at),'Inventory Base Migration');
END $$;

DO $$ DECLARE item record; d jsonb; raw_status text; next_status text; assigned_truck uuid; BEGIN
 FOR item IN SELECT * FROM inventory_assets ORDER BY id LOOP
  d:=item.data;
  IF d->>'deviceType'='samsara_camera' THEN d:=d||'{"deviceType":"dashcam","brand":"Samsara"}';
  ELSIF d->>'deviceType'='tt_camera' THEN d:=d||'{"deviceType":"dashcam","brand":"FiveELD"}';
  ELSIF d->>'deviceType'='other' AND coalesce(d->>'imei','')<>'' AND (lower(coalesce(d->>'model','')) LIKE 'iphone%' OR upper(d->>'model')='SM-A102U1') THEN d:=d||'{"deviceType":"phone"}';
  END IF;
  raw_status:=coalesce(d->>'status','');
  next_status:=CASE lower(btrim(raw_status)) WHEN 'active' THEN 'active' WHEN 'inactive' THEN 'inactive' WHEN 'sold' THEN 'sold' WHEN 'total' THEN 'total_loss' WHEN 'total_loss' THEN 'total_loss' ELSE 'unknown' END;
  IF raw_status<>'' AND lower(btrim(raw_status)) NOT IN ('active','inactive','sold','total','total_loss','unknown') AND length(coalesce(d->>'notes',''))<4500 THEN
   d:=jsonb_set(d,'{notes}',to_jsonb(concat_ws(E'\n',nullif(d->>'notes',''),'Source status retained for review: '||raw_status)));
  END IF;
  d:=jsonb_set(d,'{status}',to_jsonb(next_status));
  assigned_truck:=nullif(d->>'truckId','')::uuid;
  IF assigned_truck IS NULL AND nullif(d->>'gatewayId','') IS NOT NULL THEN SELECT truck_id INTO assigned_truck FROM inventory_assets WHERE id=(d->>'gatewayId')::uuid; END IF;
  IF assigned_truck IS NULL AND d->>'equipmentState' IN ('reported_present','verified_installed') THEN
   d:=(d-'verifiedAt'-'verifiedBy')||'{"equipmentState":"unknown"}';
  END IF;
  PERFORM pg_temp.inventory_base_update('inventory_assets','assets',item.id,d);
 END LOOP;
 FOR item IN SELECT * FROM inventory_lines ORDER BY id LOOP
  d:=item.data;raw_status:=coalesce(d->>'status','');
  next_status:=CASE WHEN lower(btrim(raw_status)) IN ('active','suspended','pending','unknown') THEN lower(btrim(raw_status)) WHEN lower(btrim(raw_status)) LIKE 'cancelled%' OR lower(btrim(raw_status)) LIKE 'canceled%' THEN 'cancelled' ELSE 'unknown' END;
  IF raw_status<>'' AND lower(btrim(raw_status)) NOT IN ('active','suspended','pending','unknown','cancelled','canceled') AND length(coalesce(d->>'notes',''))<4500 THEN
   d:=jsonb_set(d,'{notes}',to_jsonb(concat_ws(E'\n',nullif(d->>'notes',''),'Source status retained for review: '||raw_status)));
  END IF;
  d:=jsonb_set(d,'{status}',to_jsonb(next_status));
  PERFORM pg_temp.inventory_base_update('inventory_lines','lines',item.id,d);
 END LOOP;
 FOR item IN SELECT * FROM inventory_equipment WHERE data->>'name' IN ('Samsara DashCam','TT DashCam','Tablet','PrePass','PT-30/Protrux') AND coalesce((data->>'active')::boolean,true) LOOP
  d:=item.data||'{"active":false}';
  d:=jsonb_set(d,'{notes}',to_jsonb(concat_ws(E'\n',nullif(d->>'notes',''),'Legacy device checklist retired; use linked inventory devices.')));
  PERFORM pg_temp.inventory_base_update('inventory_equipment','equipment',item.id,d);
 END LOOP;
 -- An attached 2026 decal is a source report, never a physical verification.
 FOR item IN SELECT * FROM inventory_equipment WHERE data->>'name'='IFTA Sticker' AND data->>'batchYear'='2026' AND (data->>'attached')::boolean AND coalesce(data->>'equipmentState','unknown')='unknown' LOOP
  PERFORM pg_temp.inventory_base_update('inventory_equipment','equipment',item.id,item.data||'{"equipmentState":"reported_present"}');
 END LOOP;
END $$;

-- Stable supply links keep existing checkboxes attached when a supply is renamed.
DO $$ DECLARE item record; supply uuid; BEGIN
 FOR item IN SELECT * FROM inventory_equipment WHERE coalesce((data->>'active')::boolean,true) AND nullif(data->>'supplyId','') IS NULL LOOP
  SELECT id INTO supply FROM inventory_supplies
   WHERE data->>'name'=item.data->>'name' AND coalesce(data->>'batchYear','')=coalesce(item.data->>'batchYear','')
   AND coalesce((data->>'active')::boolean,true);
  IF supply IS NOT NULL THEN
   PERFORM pg_temp.inventory_base_update('inventory_equipment','equipment',item.id,jsonb_set(item.data,'{supplyId}',to_jsonb(supply::text)));
  END IF;
 END LOOP;
END $$;

DROP INDEX inventory_gateway_camera;
CREATE UNIQUE INDEX inventory_gateway_camera ON inventory_assets(gateway_id)
 WHERE data->>'deviceType'='dashcam' AND data->>'brand'='Samsara' AND coalesce((data->>'active')::boolean,true);

ALTER TABLE inventory_assets
 ADD CONSTRAINT inventory_asset_type CHECK(data->>'deviceType' IN ('tablet','phone','dashcam','samsara_gateway','pt30','other')),
 ADD CONSTRAINT inventory_dashcam_brand CHECK(
  (data->>'deviceType'='dashcam' AND coalesce(data->>'brand','') IN ('Samsara','FiveELD')) OR
  (data->>'deviceType'<>'dashcam' AND coalesce(data->>'brand','')='')),
 ADD CONSTRAINT inventory_gateway_link_type CHECK(gateway_id IS NULL OR (data->>'deviceType'='dashcam' AND data->>'brand'='Samsara')),
 ADD CONSTRAINT inventory_device_status CHECK(coalesce(data->>'status','') IN ('','unknown','active','inactive','sold','total_loss')),
 ADD CONSTRAINT inventory_device_presence CHECK(coalesce(data->>'equipmentState','') IN ('','unknown','reported_present','verified_installed','missing'));
ALTER TABLE inventory_lines ADD CONSTRAINT inventory_carrier_status CHECK(coalesce(data->>'status','') IN ('','unknown','active','suspended','cancelled','pending'));

CREATE FUNCTION inventory_installation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM inventory_assets a LEFT JOIN inventory_assets g ON g.id=a.gateway_id
  WHERE (a.id=NEW.id OR a.gateway_id=NEW.id)
   AND a.data->>'equipmentState' IN ('reported_present','verified_installed')
   AND coalesce(a.truck_id,g.truck_id) IS NULL
 ) THEN RAISE EXCEPTION 'A truck assignment is required for device installation' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER inventory_installation_guard
 AFTER INSERT OR UPDATE ON inventory_assets DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION inventory_installation_guard();

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER FUNCTION inventory_installation_guard() OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
