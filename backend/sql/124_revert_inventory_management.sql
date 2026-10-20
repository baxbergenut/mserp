BEGIN;
SET LOCAL lock_timeout = '10s';

-- Undo migration 123 for installations that received it. Fresh installations
-- already use the restored schema. Preserve the migration ledger as history.
DO $$
BEGIN
 IF to_regclass('inventory_custodians') IS NULL THEN
  RETURN;
 END IF;

 LOCK TABLE inventory_assets, inventory_lines, inventory_events,
  inventory_custodians, inventory_operations IN ACCESS EXCLUSIVE MODE;

 -- Refuse to discard any records or attributes entered since the deployment.
 IF EXISTS (SELECT 1 FROM inventory_custodians) OR
    EXISTS (SELECT 1 FROM inventory_operations) OR
    EXISTS (SELECT 1 FROM inventory_events WHERE reason<>'' OR effective_date IS NOT NULL) OR
    EXISTS (SELECT 1 FROM inventory_assets WHERE data ?| ARRAY['modelNumber','storageGb','color','caseColor','custodianId','originalModel']) THEN
  RAISE EXCEPTION 'Inventory management rollback requires review of newly recorded data';
 END IF;

 DROP TRIGGER inventory_line_lifecycle_guard ON inventory_lines;
 DROP TRIGGER inventory_lifecycle_guard ON inventory_assets;
 DROP TRIGGER inventory_preserve_details ON inventory_assets;
 DROP FUNCTION inventory_line_lifecycle_guard();
 DROP FUNCTION inventory_lifecycle_guard();
 DROP FUNCTION inventory_preserve_details();

 ALTER TABLE inventory_assets DROP CONSTRAINT inventory_holder_exclusive;
 ALTER TABLE inventory_assets DROP CONSTRAINT inventory_device_details;
 ALTER TABLE inventory_assets DROP CONSTRAINT inventory_device_status;
 ALTER TABLE inventory_assets ADD CONSTRAINT inventory_device_status
  CHECK(coalesce(data->>'status','') IN ('','unknown','active','inactive','sold','total_loss'));
 DROP INDEX inventory_asset_category;
 ALTER TABLE inventory_assets DROP COLUMN custodian_id;
 ALTER TABLE inventory_events DROP COLUMN reason, DROP COLUMN effective_date;
 DROP TABLE inventory_operations;
 DROP TABLE inventory_custodians;
END $$;

COMMIT;
