BEGIN;
CREATE TEMP TABLE trailer_status_before ON COMMIT DROP AS SELECT * FROM fleet_trailers;
-- The reviewed Stored/Unused fleet is available for use. Other legacy
-- restrictions retain their meaning; provenance is not a guessed repair reason.
UPDATE fleet_trailers SET data=(data-'truckId') || jsonb_build_object(
 'status',CASE WHEN coalesce((data->>'active')::boolean,true)=false THEN 'terminated'
  WHEN data->>'status'='retired' OR data->>'condition' IN ('retired','lost') THEN 'out_of_service'
  WHEN data->>'status' IN ('repair','needs_review') OR data->>'condition' IN ('repair','damaged') THEN 'shop'
  WHEN data->>'status' IN ('stored_unused','ready') THEN 'ready' ELSE 'shop' END,
 'statusReason',CASE WHEN coalesce((data->>'active')::boolean,true)=false THEN 'Migrated from inactive; original departure reason was not recorded.'
  WHEN data->>'status'='stored_unused' AND coalesce(data->>'condition','unknown') IN ('unknown','good') THEN 'Stored/Unused fleet confirmed Ready during the trailer status conversion.'
  ELSE 'Migrated from availability '||coalesce(nullif(data->>'status',''),'unknown')||' and condition '||coalesce(nullif(data->>'condition',''),'unknown')||'; see preserved notes and history.' END),
 version=version+1,updated_at=now();
UPDATE fleet_trailers SET data=data||jsonb_build_object('active',data->>'status' IN ('ready','in_use','shop'));
INSERT INTO inventory_events(id,request_hash,collection,record_id,action,before_data,after_data,actor_name)
 SELECT gen_random_uuid(),'migration:150:'||t.id,'trailers',t.id,'status_migration',
 jsonb_build_object('id',b.id,'version',b.version,'data',b.data,'updatedAt',b.updated_at,'updatedBy',''),
 jsonb_build_object('id',t.id,'version',t.version,'data',t.data,'updatedAt',t.updated_at,'updatedBy','Migration 150'),
 'Migration 150' FROM fleet_trailers t JOIN trailer_status_before b ON b.id=t.id;
ALTER TABLE fleet_trailers ADD COLUMN truck_id uuid GENERATED ALWAYS AS (nullif(data->>'truckId','')::uuid) STORED REFERENCES trucks(id);
CREATE UNIQUE INDEX fleet_trailer_assigned_truck ON fleet_trailers(truck_id) WHERE truck_id IS NOT NULL;
ALTER TABLE fleet_trailers ADD CONSTRAINT trailer_status_valid CHECK (
 coalesce(data->>'status','') IN ('ready','in_use','shop','out_of_service','terminated')
 AND coalesce((data->>'active')::boolean,false)=(data->>'status' IN ('ready','in_use','shop'))
 AND (data->>'status'<>'ready' OR truck_id IS NULL)
 AND (data->>'status'<>'in_use' OR truck_id IS NOT NULL)
 AND (data->>'status'<>'terminated' OR truck_id IS NULL)
 AND (data->>'status' NOT IN ('shop','out_of_service','terminated') OR length(btrim(coalesce(data->>'statusReason','')))>0)
);
COMMIT;
