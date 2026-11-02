-- Reviewed operational writes must neither rebuild nor serialize payroll.
-- Quiesce all writers and install the matching deployment helper before applying.
BEGIN;
SELECT pg_advisory_xact_lock(736281941);
UPDATE payroll_projection_sources SET classification='unrelated'
WHERE table_name IN (
 'access_audit','app_roles','role_expense_category_access','phone_normalization_audit',
 'feedback_attachments','feedback_comments','feedback_messages','feedback_posts',
 'feedback_status_history','feedback_votes','fleet_trailers','inventory_assets',
 'inventory_equipment','inventory_events','inventory_imports','inventory_lines',
 'inventory_locations','inventory_stock_movements','inventory_supplies','inventory_transponder_notes',
 'maintenance_changes','maintenance_configuration','maintenance_documents','maintenance_odometer_readings',
 'maintenance_pm_baselines','maintenance_reading_changes','maintenance_records','maintenance_schedules',
 'maintenance_types','maintenance_vendors','service_programs','service_program_trucks',
 'expense_documents','expense_sheet_export_config','expense_sheet_exports','files',
 'fleetscope_driver_intake','fleetscope_driver_terminations','fleetscope_webhook_receipts','prepass_toll_sync_days',
 'relay_fuel_sync_days','prepass_truck_mapping_events','toll_flag_review_events','toll_reports',
 'toll_transaction_links','weighmytruck_audit','weighmytruck_billing','weighmytruck_events',
 'weighmytruck_imports','weighmytruck_memberships','weighmytruck_requests','weighmytruck_sync_days',
 'dispatcher_commission_rules','dispatcher_pay_history','dispatcher_pay_inputs','dispatcher_pay_statements');
DO $$ DECLARE t text; trigger_name text; BEGIN
 FOREACH t IN ARRAY ARRAY[
 'access_audit','app_roles','role_expense_category_access','phone_normalization_audit',
 'feedback_attachments','feedback_comments','feedback_messages','feedback_posts',
 'feedback_status_history','feedback_votes','fleet_trailers','inventory_assets',
 'inventory_equipment','inventory_events','inventory_imports','inventory_lines',
 'inventory_locations','inventory_stock_movements','inventory_supplies','inventory_transponder_notes',
 'maintenance_changes','maintenance_configuration','maintenance_documents','maintenance_odometer_readings',
 'maintenance_pm_baselines','maintenance_reading_changes','maintenance_records','maintenance_schedules',
 'maintenance_types','maintenance_vendors','service_programs','service_program_trucks',
 'expense_documents','expense_sheet_export_config','expense_sheet_exports','files',
 'fleetscope_driver_intake','fleetscope_driver_terminations','fleetscope_webhook_receipts','prepass_toll_sync_days',
 'relay_fuel_sync_days','prepass_truck_mapping_events','toll_flag_review_events','toll_reports',
 'toll_transaction_links','weighmytruck_audit','weighmytruck_billing','weighmytruck_events',
 'weighmytruck_imports','weighmytruck_memberships','weighmytruck_requests','weighmytruck_sync_days',
 'dispatcher_commission_rules','dispatcher_pay_history','dispatcher_pay_inputs','dispatcher_pay_statements'] LOOP
  FOREACH trigger_name IN ARRAY ARRAY['payroll_protocol_statement','payroll_protocol_row',
   'payroll_projection_insert','payroll_projection_update','payroll_projection_delete','payroll_projection_truncate'] LOOP
   EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I',trigger_name,t);
  END LOOP;
 END LOOP;
END $$;

-- Actor labels use username. Security/appearance updates do not change a label.
-- Email/role/active stay protected in the bypass because the account synchronizer
-- can write canonical dispatcher data when these fields change.
CREATE FUNCTION payroll_user_metadata_change(prior jsonb, following jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT prior IS NOT NULL AND following IS NOT NULL AND
 (prior-ARRAY['theme','password_hash','version','updated_at']) IS NOT DISTINCT FROM
 (following-ARRAY['theme','password_hash','version','updated_at'])
$$;
DO $$ DECLARE definition text; prefix text; BEGIN
 prefix:=$prefix$BEGIN
 IF TG_TABLE_NAME='app_users' AND TG_OP='UPDATE' AND current_setting('mserp.payroll.user_metadata',true)='on' THEN RETURN NULL; END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('guard_payroll_protocol_statement()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',prefix);
 prefix:=$prefix$BEGIN
 IF TG_TABLE_NAME='app_users' AND TG_OP='UPDATE' THEN
  IF NOT EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
   WHERE NOT payroll_user_metadata_change(to_jsonb(o),to_jsonb(n))) THEN RETURN NULL; END IF;
 END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('capture_payroll_projection_change()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',prefix);
END $$;
-- Theme/password/session changes must not reach even an empty dispatcher UPDATE:
-- its statement trigger would otherwise still acquire the payroll source gate.
DO $$ DECLARE definition text; BEGIN
 definition:=replace(pg_get_functiondef('sync_dispatcher_account()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',E'BEGIN\n IF TG_TABLE_NAME=''app_users'' AND payroll_user_metadata_change(to_jsonb(OLD),to_jsonb(NEW)) THEN RETURN NEW; END IF;\n');
END $$;
CREATE FUNCTION guard_payroll_user_metadata() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('mserp.payroll.user_metadata',true)='on' AND
  (TG_OP<>'UPDATE' OR NOT payroll_user_metadata_change(to_jsonb(OLD),to_jsonb(NEW))) THEN
  RAISE EXCEPTION 'Account metadata cannot change payroll identity or dispatcher data' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER zz_payroll_user_metadata BEFORE INSERT OR UPDATE OR DELETE ON app_users
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_user_metadata();
CREATE FUNCTION invalidate_payroll_user_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
  WHERE NOT payroll_user_metadata_change(to_jsonb(o),to_jsonb(n))) THEN
  UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='app_users';
 END IF;
 RETURN NULL;
END $$;
DROP TRIGGER payroll_cache_update ON app_users;
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON app_users
 REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_user_cache();

-- Re-fetching unchanged tickets/tolls must not rebuild payroll. Compare every
-- column, including provider payloads and future columns, except sync bookkeeping.
DO $$ DECLARE definition text; prefix text; BEGIN
 prefix:=$prefix$BEGIN
 IF TG_OP='UPDATE' AND TG_TABLE_NAME IN ('tolls','weighmytruck_transactions') THEN
  IF NOT EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
   WHERE (to_jsonb(o)-ARRAY['synced_at','location_synced_at']) IS DISTINCT FROM (to_jsonb(n)-ARRAY['synced_at','location_synced_at'])) THEN RETURN NULL; END IF;
 END IF;
$prefix$;
 definition:=replace(pg_get_functiondef('capture_payroll_projection_change()'::regprocedure),E'\r\n',E'\n');
 EXECUTE regexp_replace(definition,E'BEGIN\n',prefix);
END $$;
CREATE FUNCTION invalidate_payroll_import_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
  WHERE (to_jsonb(o)-ARRAY['synced_at','location_synced_at']) IS DISTINCT FROM (to_jsonb(n)-ARRAY['synced_at','location_synced_at'])) THEN
  IF payroll_resource_command() AND payroll_resource_table(TG_TABLE_NAME) THEN RETURN NULL; END IF;
  UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name=TG_TABLE_NAME;
 END IF;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY[
 'tolls','weighmytruck_transactions'] LOOP
  EXECUTE format('DROP TRIGGER payroll_cache_update ON %I',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_import_cache()',t);
 END LOOP;
END $$;

-- Cache revisions and all operational audit receipts remain intact. Rebuild once
-- against the smaller source fingerprint after activation.
UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,
 impact_roots=NULL,lease_token=NULL,lease_until=NULL,
 requested_at=clock_timestamp(),available_at=clock_timestamp(),attempts=0,last_error='';
COMMIT;
