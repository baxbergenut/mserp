BEGIN;

-- Additive shadow storage. Canonical ledgers and frozen settlement reports remain
-- authoritative. Enabling capture is an explicit application capability.
CREATE TABLE payroll_projection_control (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    capture_enabled boolean NOT NULL DEFAULT false,
    protocol_mode text NOT NULL DEFAULT 'global' CHECK(protocol_mode IN ('global','dual','resources')),
    protocol_epoch bigint NOT NULL DEFAULT 1 CHECK(protocol_epoch>0)
);
INSERT INTO payroll_projection_control DEFAULT VALUES;
CREATE TABLE payroll_protocol_events (
    protocol_epoch bigint PRIMARY KEY,
    previous_mode text NOT NULL CHECK(previous_mode IN ('global','dual','resources')),
    protocol_mode text NOT NULL CHECK(protocol_mode IN ('global','dual','resources')),
    capability text NOT NULL,
    switched_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE payroll_resource_revisions (
    resource_key text PRIMARY KEY,
    revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0)
);
CREATE TABLE payroll_projection_sources (
    table_name text PRIMARY KEY,
    classification text NOT NULL CHECK (classification IN ('financial','display','unrelated','derived'))
);
CREATE TABLE payroll_projection_changes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
    table_name text NOT NULL REFERENCES payroll_projection_sources(table_name),
    operation text NOT NULL CHECK (operation IN ('INSERT','UPDATE','DELETE','TRUNCATE')),
    revision_only boolean NOT NULL DEFAULT false,
    source_revision bigint NOT NULL,
    old_routes jsonb NOT NULL DEFAULT '[]',
    new_routes jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    dispatched_at timestamptz
);
CREATE INDEX payroll_projection_changes_pending ON payroll_projection_changes(id) WHERE dispatched_at IS NULL;
CREATE INDEX payroll_projection_changes_transaction ON payroll_projection_changes(source_xid);
CREATE INDEX payroll_projection_changes_source_revision ON payroll_projection_changes(table_name,source_revision);
CREATE TABLE payroll_projection_jobs (
    week_start date PRIMARY KEY CHECK (extract(isodow FROM week_start)=1),
    requested_generation bigint NOT NULL DEFAULT 1,
    completed_generation bigint NOT NULL DEFAULT 0,
    impact_roots text[], -- NULL requires full preparation; otherwise union of captured old/new principals.
    priority integer NOT NULL DEFAULT 0,
    interactive_until timestamptz, -- Short-lived priority for currently viewed weeks.
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    CHECK (completed_generation<=requested_generation),
    CHECK ((lease_token IS NULL)=(lease_until IS NULL))
);
CREATE INDEX payroll_projection_jobs_pending ON payroll_projection_jobs(priority DESC,available_at,week_start)
    WHERE requested_generation>completed_generation;
CREATE TABLE payroll_projection_generations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    week_start date NOT NULL,
    requested_generation bigint NOT NULL,
    input_snapshot text NOT NULL,
    calendar_week date NOT NULL,
    calculator_version text NOT NULL,
    schema_version integer NOT NULL,
    dependencies jsonb NOT NULL,
    driver_header jsonb NOT NULL,
    investor_header jsonb NOT NULL,
    prepared_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(id,week_start)
);
CREATE TABLE payroll_load_facts (
    load_id bigint NOT NULL,
    source_hash text NOT NULL,
    calculator_version text NOT NULL,
    trip_count bigint NOT NULL,
    trips_valid boolean,
    trip_allocation jsonb,
    trip_values jsonb,
    prepared_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(load_id,source_hash,calculator_version)
);
-- Reusable dated source facts. Financial edits and settlement overlays are
-- deliberately excluded. The exact source revision key survives API restarts.
CREATE TABLE payroll_routing_facts (
    kind text NOT NULL CHECK(kind IN ('load_trucks','truck_costs','driver_base','driver_cost_base','native_costs','vacations','stopped_charges','selection_fuel','driver_days','investor_days','load_cohort')),
    week_start date NOT NULL,
    calculator_version text NOT NULL,
    input_fingerprint text NOT NULL,
    entity_id text NOT NULL DEFAULT '',
    payload jsonb NOT NULL,
    PRIMARY KEY(kind,week_start,calculator_version,input_fingerprint,entity_id)
);
CREATE TABLE payroll_statement_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    generation_id uuid NOT NULL,
    kind text NOT NULL CHECK(kind IN ('driver','investor')),
    statement_id uuid NOT NULL,
    week_start date NOT NULL,
    detail jsonb NOT NULL,
    cost_evidence jsonb NOT NULL,
    resource_dependencies jsonb NOT NULL DEFAULT '{}',
    summary jsonb NOT NULL,
    fee_cents bigint NOT NULL,
    payable_cents bigint NOT NULL,
    finalized boolean NOT NULL,
    edit_version integer NOT NULL,
    settlement_version integer NOT NULL,
    UNIQUE(generation_id,kind,statement_id),
    UNIQUE(id,kind,statement_id,week_start),
    FOREIGN KEY(generation_id,week_start) REFERENCES payroll_projection_generations(id,week_start)
);
CREATE INDEX payroll_statement_versions_history ON payroll_statement_versions(kind,statement_id,week_start,generation_id);
CREATE TABLE payroll_statement_state (
    kind text NOT NULL,
    statement_id uuid NOT NULL,
    week_start date NOT NULL,
    version_id uuid NOT NULL,
    PRIMARY KEY(kind,statement_id,week_start),
    FOREIGN KEY(version_id,kind,statement_id,week_start) REFERENCES payroll_statement_versions(id,kind,statement_id,week_start)
);
CREATE INDEX payroll_statement_state_week ON payroll_statement_state(kind,week_start,statement_id);
CREATE TABLE payroll_projection_weeks (
    week_start date PRIMARY KEY,
    generation_id uuid NOT NULL,
    FOREIGN KEY(generation_id,week_start) REFERENCES payroll_projection_generations(id,week_start)
);
CREATE TABLE payroll_command_receipts (
    actor_id uuid NOT NULL,
    request_id uuid NOT NULL,
    command text NOT NULL,
    request_hash text NOT NULL,
    expected_revision text NOT NULL,
    result jsonb NOT NULL,
    affected_statements jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(actor_id,request_id)
);
CREATE TABLE payroll_command_versions (
    actor_id uuid NOT NULL,
    request_id uuid NOT NULL,
    version_id uuid NOT NULL REFERENCES payroll_statement_versions(id),
    PRIMARY KEY(actor_id,request_id,version_id),
    FOREIGN KEY(actor_id,request_id) REFERENCES payroll_command_receipts(actor_id,request_id)
);

CREATE FUNCTION guard_payroll_projection_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'Payroll projection versions and command receipts are immutable' USING ERRCODE='23514';
END $$;
CREATE TRIGGER payroll_projection_generation_immutable BEFORE UPDATE OR DELETE ON payroll_projection_generations
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_projection_history();
CREATE TRIGGER payroll_statement_version_immutable BEFORE UPDATE OR DELETE ON payroll_statement_versions
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_projection_history();
CREATE TRIGGER payroll_command_receipt_immutable BEFORE UPDATE OR DELETE ON payroll_command_receipts
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_projection_history();


-- Exact month buckets retain bases and collections separately. Current/future
-- weeks and the month crossing the first-live boundary remain individual rows.
CREATE TABLE payroll_cost_months (
 driver_id uuid NOT NULL, month_start date NOT NULL,
 fuel_base numeric NOT NULL DEFAULT 0,toll_base numeric NOT NULL DEFAULT 0,
 fuel_amount numeric NOT NULL DEFAULT 0,toll_amount numeric NOT NULL DEFAULT 0,
 row_count bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(driver_id,month_start)
);
CREATE TABLE payroll_source_months (
 source text NOT NULL,owner_id uuid NOT NULL,month_start date NOT NULL,
 base numeric NOT NULL DEFAULT 0,amount numeric NOT NULL DEFAULT 0,
 version_sum bigint NOT NULL DEFAULT 0,row_count bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(owner_id,source,month_start)
);
INSERT INTO payroll_cost_months SELECT driver_id,date_trunc('month',week_start)::date,sum(fuel_base),sum(toll_base),sum(fuel_amount),sum(toll_amount),count(*) FROM driver_pay_cost_collections GROUP BY driver_id,date_trunc('month',week_start)::date;
INSERT INTO payroll_source_months SELECT source,owner_id,date_trunc('month',week_start)::date,sum(base),sum(amount),sum(version),count(*) FROM pay_source_collections GROUP BY source,owner_id,date_trunc('month',week_start)::date;
CREATE FUNCTION maintain_payroll_balance_months() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v jsonb; sign integer; m date;
BEGIN
 IF TG_OP='TRUNCATE' THEN
  IF TG_TABLE_NAME='driver_pay_cost_collections' THEN DELETE FROM payroll_cost_months; ELSE DELETE FROM payroll_source_months; END IF;
  RETURN NULL;
 END IF;
 -- Apply deltas under the bucket's row lock. Concurrent contributions cannot
 -- overwrite another transaction's amount with a stale aggregate.
 FOR v,sign IN SELECT to_jsonb(OLD),-1 WHERE TG_OP IN ('UPDATE','DELETE')
  UNION ALL SELECT to_jsonb(NEW),1 WHERE TG_OP IN ('INSERT','UPDATE') LOOP
  m=date_trunc('month',(v->>'week_start')::date)::date;
  IF TG_TABLE_NAME='driver_pay_cost_collections' THEN
   INSERT INTO payroll_cost_months(driver_id,month_start,fuel_base,toll_base,fuel_amount,toll_amount,row_count)
   VALUES((v->>'driver_id')::uuid,m,sign*(v->>'fuel_base')::numeric,sign*(v->>'toll_base')::numeric,sign*(v->>'fuel_amount')::numeric,sign*(v->>'toll_amount')::numeric,sign)
   ON CONFLICT(driver_id,month_start) DO UPDATE SET fuel_base=payroll_cost_months.fuel_base+excluded.fuel_base,toll_base=payroll_cost_months.toll_base+excluded.toll_base,
    fuel_amount=payroll_cost_months.fuel_amount+excluded.fuel_amount,toll_amount=payroll_cost_months.toll_amount+excluded.toll_amount,row_count=payroll_cost_months.row_count+excluded.row_count;
  ELSE
   INSERT INTO payroll_source_months(source,owner_id,month_start,base,amount,version_sum,row_count)
   VALUES(v->>'source',(v->>'owner_id')::uuid,m,sign*(v->>'base')::numeric,sign*(v->>'amount')::numeric,sign*(v->>'version')::bigint,sign)
   ON CONFLICT(owner_id,source,month_start) DO UPDATE SET base=payroll_source_months.base+excluded.base,amount=payroll_source_months.amount+excluded.amount,
    version_sum=payroll_source_months.version_sum+excluded.version_sum,row_count=payroll_source_months.row_count+excluded.row_count;
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
CREATE TRIGGER payroll_balance_rows AFTER INSERT OR UPDATE OR DELETE ON driver_pay_cost_collections FOR EACH ROW EXECUTE FUNCTION maintain_payroll_balance_months();
CREATE TRIGGER payroll_balance_truncate AFTER TRUNCATE ON driver_pay_cost_collections FOR EACH STATEMENT EXECUTE FUNCTION maintain_payroll_balance_months();
CREATE TRIGGER payroll_balance_rows AFTER INSERT OR UPDATE OR DELETE ON pay_source_collections FOR EACH ROW EXECUTE FUNCTION maintain_payroll_balance_months();
CREATE TRIGGER payroll_balance_truncate AFTER TRUNCATE ON pay_source_collections FOR EACH STATEMENT EXECUTE FUNCTION maintain_payroll_balance_months();
CREATE TRIGGER payroll_load_fact_immutable BEFORE UPDATE OR DELETE ON payroll_load_facts FOR EACH ROW EXECUTE FUNCTION guard_payroll_projection_history();
CREATE TRIGGER payroll_routing_fact_immutable BEFORE UPDATE OR DELETE ON payroll_routing_facts FOR EACH ROW EXECUTE FUNCTION guard_payroll_projection_history();

-- Retain only routing identities and effective dates, never whole source rows,
-- credentials, private profiles, provider payloads, or source document contents.
CREATE FUNCTION payroll_projection_route(row_value jsonb) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT coalesce(jsonb_object_agg(key,value),'{}'::jsonb) FROM jsonb_each(row_value)
 WHERE key=ANY(ARRAY['id','driver_id','main_driver_id','team_driver_id','truck_id','owner_id','investor_id','entity_kind','entity_id','related_truck_id','effective_at',
 'charge_driver_id','charge_truck_id','investor_truck_id','kind','statement_id','source','expense_id','escrow_id','schedule_id','type_id','load_record_id','load_trip_id','load_id','fuel_transaction_id','reported_truck_units',
 'source_driver_id','target_driver_id','from_driver_id','to_driver_id','from_week','to_week',
 'week_start','start_week','end_week','source_week','target_week','service_date','source_date','dispatch_date','settlement_date','expense_date','transaction_date',
 'purchased_on','posting_date','exit_date','pickup_time','assigned_at','unassigned_at','start_date','end_date',
 'hire_date','termination_date','working_start_date','finalized','active','deleted','covered_by','broker_load_number'])
$$;
-- Placement audit images are payroll inputs themselves (removed-week history).
-- Keep each old/new destination without retaining the financial payload or note.
CREATE FUNCTION payroll_projection_row_routes(table_value text,row_value jsonb) RETURNS SETOF jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT payroll_projection_route(image) FROM (
 SELECT row_value image WHERE table_value NOT IN ('payroll_load_placement_history','gross_board_transfer_history')
 UNION ALL SELECT row_value->'before_value' WHERE table_value IN ('payroll_load_placement_history','gross_board_transfer_history')
 UNION ALL SELECT row_value->'after_value' WHERE table_value IN ('payroll_load_placement_history','gross_board_transfer_history')
 ) images WHERE jsonb_typeof(image)='object'
$$;

-- Dual retains the exclusive gate and table revisions. Resources uses a shared
-- source gate plus exclusive component locks; source writers stay exclusive.
CREATE FUNCTION payroll_resource_table(table_value text) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT table_value=ANY(ARRAY['driver_pay_weeks','driver_pay_cost_collections','investor_pay_weeks',
 'pay_source_collections','payroll_settlements','payroll_settlement_events','truck_settlement_events',
 'expense_payments','expense_reimbursement_payments','driver_escrow_payments',
 'driver_charge_occurrences','driver_charge_events','driver_charge_phases',
 'expenses','driver_escrows','driver_charge_schedules'])
$$;
CREATE FUNCTION payroll_row_resources(table_value text,row_value jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT ARRAY(SELECT DISTINCT resource FROM (
 SELECT CASE key WHEN 'driver_id' THEN 'driver:' WHEN 'charge_driver_id' THEN 'driver:'
 WHEN 'truck_id' THEN 'truck:' WHEN 'investor_truck_id' THEN 'truck:' WHEN 'owner_id' THEN 'owner:'
 WHEN 'expense_id' THEN 'expense:' WHEN 'escrow_id' THEN 'escrow:' WHEN 'schedule_id' THEN 'schedule:' END||value resource
 FROM jsonb_each_text(row_value) WHERE key IN ('driver_id','charge_driver_id','truck_id','investor_truck_id','owner_id','expense_id','escrow_id','schedule_id')
 UNION ALL SELECT CASE table_value WHEN 'expenses' THEN 'expense:' WHEN 'driver_escrows' THEN 'escrow:'
 WHEN 'driver_charge_schedules' THEN 'schedule:' END||(row_value->>'id')
 UNION ALL SELECT CASE row_value->>'kind' WHEN 'driver' THEN 'driver:' WHEN 'investor' THEN 'truck:' END||(row_value->>'statement_id')
 ) keys WHERE resource IS NOT NULL ORDER BY resource)
$$;
CREATE FUNCTION payroll_row_revision_keys(table_value text,row_value jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN table_value IN ('driver_pay_weeks','payroll_settlements','payroll_settlement_events') THEN
 ARRAY['statement:driver:'||(row_value->>'driver_id')||':'||(row_value->>'week_start')]
 WHEN table_value IN ('investor_pay_weeks','truck_settlement_events') THEN
 ARRAY['statement:investor:'||(row_value->>'truck_id')||':'||(row_value->>'week_start')]
 ELSE payroll_row_resources(table_value,row_value) END
$$;
CREATE FUNCTION payroll_resource_snapshot(keys text[]) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT coalesce(jsonb_object_agg(key,coalesce(r.revision,0)),'{}') FROM unnest(keys) key
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=key
$$;
CREATE FUNCTION payroll_financial_snapshot(source_snapshot text) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT source_snapshot||CASE WHEN protocol_mode='resources' THEN
 ':'||coalesce((SELECT md5(string_agg(resource_key||':'||revision::text,',' ORDER BY resource_key)) FROM payroll_resource_revisions),'') ELSE '' END
 FROM payroll_projection_control WHERE singleton
$$;
CREATE FUNCTION payroll_resources_current(dependencies jsonb) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT ((SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton) OR dependencies<>'{}')
 AND NOT EXISTS(SELECT 1 FROM jsonb_each_text(dependencies) d(key,value)
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=d.key WHERE coalesce(r.revision,0)<>d.value::bigint)
$$;
CREATE FUNCTION payroll_assert_resources(required_keys text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE claimed text[]; epoch bigint; protocol text;
BEGIN
 SELECT protocol_epoch,protocol_mode INTO STRICT epoch,protocol FROM payroll_projection_control WHERE singleton;
 IF coalesce(current_setting('mserp.payroll.protocol_epoch',true),'')<>epoch::text THEN
  RAISE EXCEPTION 'Payroll protocol epoch changed' USING ERRCODE='40001';
 END IF;
 SELECT array_agg(value) INTO claimed FROM jsonb_array_elements_text(coalesce(nullif(current_setting('mserp.payroll.resources',true),''),'[]')::jsonb);
 IF coalesce(cardinality(required_keys),0)=0 OR NOT coalesce(required_keys<@claimed,false) THEN
  RAISE EXCEPTION 'Payroll resource claim is incomplete' USING ERRCODE='40001';
 END IF;
 -- A resource command cannot run alongside a source/administrative writer.
 IF NOT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
  AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=0 AND objid=736281940
  AND objsubid=1 AND (mode='ExclusiveLock' OR (protocol='resources' AND mode='ShareLock')) AND granted) THEN
  RAISE EXCEPTION 'Payroll domain gate is missing' USING ERRCODE='40001';
 END IF;
 IF EXISTS(WITH held AS MATERIALIZED (
  SELECT classid,objid FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
  AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
  AND objsubid=1 AND mode='ExclusiveLock' AND granted
 ), required AS (SELECT hashtextextended(resource,182736281940) id FROM unnest(required_keys) resource)
 SELECT 1 FROM required r WHERE NOT EXISTS(SELECT 1 FROM held h
 WHERE h.classid=((r.id>>32)&4294967295)::oid AND h.objid=(r.id&4294967295)::oid)) THEN
  RAISE EXCEPTION 'Payroll resource lock is missing' USING ERRCODE='40001';
 END IF;
END $$;
-- One server call, still the same deterministic order of actual advisory IDs.
-- Session locks bridge discovery and the new financial snapshot. A failed
-- acquisition requires closing the connection to release any partial set.
CREATE FUNCTION payroll_lock_resources(keys text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE resource bigint;
BEGIN
 FOR resource IN SELECT DISTINCT hashtextextended(key,182736281940) FROM unnest(keys) key ORDER BY 1 LOOP
  PERFORM pg_advisory_lock(resource);
 END LOOP;
END $$;
CREATE FUNCTION payroll_unlock_resources(keys text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE resource bigint;
BEGIN
 FOR resource IN SELECT DISTINCT hashtextextended(key,182736281940) FROM unnest(keys) key ORDER BY 1 LOOP
  PERFORM pg_advisory_unlock(resource);
 END LOOP;
END $$;
CREATE FUNCTION payroll_resource_command() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT (SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton)
 AND coalesce(current_setting('mserp.payroll.resources',true),'')<>''
$$;
CREATE FUNCTION payroll_week_revision(week_value date,kind_value text,generation uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN generation IS NULL THEN NULL
 WHEN (SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton) THEN
 md5(generation::text||':'||kind_value||':'||coalesce((SELECT string_agg(statement_id::text||':'||version_id::text,',' ORDER BY statement_id)
 FROM payroll_statement_state WHERE week_start=week_value AND kind=kind_value),''))::uuid::text
 ELSE generation::text END
$$;
CREATE FUNCTION payroll_financial_gate(shared boolean DEFAULT false) RETURNS void LANGUAGE plpgsql AS $$
DECLARE keys text[];
BEGIN
 IF payroll_resource_command() THEN
  SELECT array_agg(value) INTO keys FROM jsonb_array_elements_text(current_setting('mserp.payroll.resources')::jsonb);
  PERFORM payroll_assert_resources(keys);
 ELSIF shared THEN PERFORM pg_advisory_xact_lock_shared(736281940);
 ELSE PERFORM pg_advisory_xact_lock(736281940);
 END IF;
END $$;
-- Preserve every existing collection/finalization guard. Only replace its
-- domain-lock entry; row guards still validate each old/new resource separately.
DO $$ DECLARE f record; definition text; BEGIN
 FOR f IN SELECT oid FROM pg_proc WHERE pronamespace=current_schema()::regnamespace
 AND prokind='f' AND proname NOT IN ('payroll_assert_resources','payroll_financial_gate')
 AND (strpos(prosrc,'pg_advisory_xact_lock(736281940)')>0 OR strpos(prosrc,'pg_advisory_xact_lock_shared(736281940)')>0) LOOP
  definition:=pg_get_functiondef(f.oid);
  definition:=replace(definition,'pg_advisory_xact_lock(736281940)','payroll_financial_gate(false)');
  definition:=replace(definition,'pg_advisory_xact_lock_shared(736281940)','payroll_financial_gate(true)');
  EXECUTE definition;
 END LOOP;
END $$;
CREATE OR REPLACE FUNCTION invalidate_payroll_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'TRUNCATE' THEN
  IF NOT EXISTS(SELECT 1 FROM changed_rows LIMIT 1) THEN RETURN NULL; END IF;
 END IF;
 IF payroll_resource_command() AND payroll_resource_table(TG_TABLE_NAME) THEN
  -- BEFORE row guards validated all claims. The capture trigger advances exact
  -- resource epochs in this transaction; independent commands share no counter.
  RETURN NULL;
 END IF;
 UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name=TG_TABLE_NAME;
 RETURN NULL;
END $$;
CREATE FUNCTION guard_payroll_protocol_statement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE protocol text; captured boolean;
BEGIN
 SELECT protocol_mode,capture_enabled INTO STRICT protocol,captured FROM payroll_projection_control WHERE singleton;
 IF protocol='global' THEN RETURN NULL; END IF;
 IF NOT captured THEN RAISE EXCEPTION 'Payroll protocol requires durable capture' USING ERRCODE='40001'; END IF;
 IF coalesce(current_setting('mserp.payroll.resources',true),'')<>'' THEN
  IF TG_OP='TRUNCATE' OR NOT payroll_resource_table(TG_TABLE_NAME) THEN
   RAISE EXCEPTION 'Payroll command attempted an unregistered source mutation' USING ERRCODE='40001';
  END IF;
 ELSE
  -- Direct SQL, imports and source mutations participate through the same gate.
  -- Multi-statement callers must enter it before taking their own row locks.
  PERFORM pg_advisory_xact_lock(736281940);
 END IF;
 RETURN NULL;
END $$;
CREATE FUNCTION guard_payroll_protocol_row() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior jsonb; following jsonb; resources text[];
BEGIN
 IF (SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton)
  OR coalesce(current_setting('mserp.payroll.resources',true),'')='' THEN
  IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
 END IF;
 prior:=CASE WHEN TG_OP<>'INSERT' THEN to_jsonb(OLD) ELSE '{}'::jsonb END;
 following:=CASE WHEN TG_OP<>'DELETE' THEN to_jsonb(NEW) ELSE '{}'::jsonb END;
 IF TG_TABLE_NAME IN ('expenses','driver_escrows','driver_charge_schedules') AND
  (TG_OP<>'UPDATE' OR prior-ARRAY['balance_version','version','updated_at'] IS DISTINCT FROM following-ARRAY['balance_version','version','updated_at']) THEN
  RAISE EXCEPTION 'Payroll command attempted a principal edit' USING ERRCODE='40001';
 END IF;
 SELECT ARRAY(SELECT DISTINCT key FROM unnest(payroll_row_resources(TG_TABLE_NAME,prior)||payroll_row_resources(TG_TABLE_NAME,following)) key) INTO resources;
 PERFORM payroll_assert_resources(resources);
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE FUNCTION advance_payroll_resource_revisions(table_value text,routes jsonb) RETURNS void LANGUAGE plpgsql AS $$
DECLARE resource text; keys text[];
BEGIN
 IF (SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton)
  OR NOT payroll_resource_table(table_value) THEN RETURN; END IF;
 SELECT ARRAY(SELECT DISTINCT key FROM jsonb_array_elements(routes) route
 CROSS JOIN LATERAL unnest(payroll_row_revision_keys(table_value,route)) key WHERE key IS NOT NULL) INTO keys;
 -- Source writers hold the exclusive gate; claimed commands additionally hold
 -- all resource locks. One statement advances each key once, in lock order.
 FOR resource IN SELECT key FROM unnest(keys) key ORDER BY hashtextextended(key,182736281940),key LOOP
  INSERT INTO payroll_resource_revisions(resource_key,revision) VALUES(resource,1)
  ON CONFLICT(resource_key) DO UPDATE SET revision=payroll_resource_revisions.revision+1;
 END LOOP;
END $$;
CREATE FUNCTION capture_payroll_projection_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_values jsonb='[]'; new_values jsonb='[]'; rev bigint; revision_only boolean=false;
BEGIN
 IF NOT (SELECT capture_enabled FROM payroll_projection_control WHERE singleton) THEN RETURN NULL; END IF;
 IF TG_OP='UPDATE' AND TG_TABLE_NAME='loads' THEN
  IF NOT EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
    WHERE (to_jsonb(o)-'synced_at') IS DISTINCT FROM (to_jsonb(n)-'synced_at')) THEN RETURN NULL; END IF;
 END IF;
 IF TG_OP='UPDATE' AND TG_TABLE_NAME IN ('expenses','driver_escrows','driver_charge_schedules') THEN
  -- Exact row comparison includes identity and every substantive column. A
  -- collection's revision bump cannot hide a simultaneous financial edit.
  SELECT NOT EXISTS(SELECT 1 FROM previous_rows o FULL JOIN changed_rows n USING(id)
   WHERE (to_jsonb(o)-ARRAY['balance_version','version','updated_at']) IS DISTINCT FROM
         (to_jsonb(n)-ARRAY['balance_version','version','updated_at'])) INTO revision_only;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') THEN
  SELECT coalesce(jsonb_agg(DISTINCT image),'[]') INTO old_values FROM previous_rows r
   CROSS JOIN LATERAL payroll_projection_row_routes(TG_TABLE_NAME,to_jsonb(r)) image;
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN
  SELECT coalesce(jsonb_agg(DISTINCT image),'[]') INTO new_values FROM changed_rows r
   CROSS JOIN LATERAL payroll_projection_row_routes(TG_TABLE_NAME,to_jsonb(r)) image;
 END IF;
 IF TG_OP<>'TRUNCATE' AND old_values='[]' AND new_values='[]' THEN RETURN NULL; END IF;
 SELECT revision INTO STRICT rev FROM payroll_cache_revisions WHERE table_name=TG_TABLE_NAME;
 INSERT INTO payroll_projection_changes(table_name,operation,source_revision,old_routes,new_routes,revision_only)
 VALUES(TG_TABLE_NAME,TG_OP,rev,old_values,new_values,revision_only);
 PERFORM advance_payroll_resource_revisions(TG_TABLE_NAME,old_values||new_values);
 PERFORM pg_notify('payroll_projection_work','');
 RETURN NULL;
END $$;

-- Commit-only refresh hints. Derived writes never enqueue source work or advance
-- canonical revisions. The fallback reader still validates every dependency.
CREATE FUNCTION notify_payroll_projection_visible() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT capture_enabled FROM payroll_projection_control WHERE singleton) THEN
  PERFORM pg_notify('payroll_projection_visible','');
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER payroll_visible_sources AFTER INSERT ON payroll_projection_changes
 FOR EACH STATEMENT EXECUTE FUNCTION notify_payroll_projection_visible();
CREATE TRIGGER payroll_visible_statements AFTER INSERT OR UPDATE OR DELETE ON payroll_statement_state
 FOR EACH STATEMENT EXECUTE FUNCTION notify_payroll_projection_visible();
CREATE TRIGGER payroll_visible_weeks AFTER INSERT OR UPDATE OR DELETE ON payroll_projection_weeks
 FOR EACH STATEMENT EXECUTE FUNCTION notify_payroll_projection_visible();

-- Unknown future tables are deliberately NOT enrolled automatically. Coverage
-- validation must refuse publication if a migration omits classification/capture.
INSERT INTO payroll_projection_sources(table_name,classification)
 SELECT table_name, CASE WHEN table_name=ANY(ARRAY[
 'status_color_settings','status_color_history','accounting_cutover_previews','accounting_payroll_archives',
 'driver_board_driver_order','driver_board_dispatcher_order','custom_tasks','system_task_records','system_task_assignments',
 'system_task_assignment_members','five_eld_locations','five_eld_sync_state','five_eld_truck_reviews',
 'samsara_telemetry','samsara_tracking','samsara_sync_state','maintenance_odometer_sources',
 'driver_service_requests','driver_service_events','profile_notes','driver_board_presence','gross_board_presence','driver_board_automation','fleetscope_sync_state',
 'fleetscope_sync_links','fleetscope_sync_snapshots','fleetscope_sync_operations','fleetscope_sync_audit',
 'datatruck_enrollments','datatruck_enrollment_events','datatruck_driver_updates','datatruck_driver_update_events',
 'relay_driver_rosters','relay_driver_operations','prepass_fleet_rosters','prepass_fleet_operations','prepass_fleet_links'
 ]) THEN 'unrelated' WHEN table_name='app_users' THEN 'display' ELSE 'financial' END
 FROM payroll_cache_revisions;
INSERT INTO payroll_projection_sources(table_name,classification) VALUES
 ('payroll_cache_revisions','derived'),('schema_migrations','unrelated'),('auth_sessions','unrelated'),('user_login_activity','unrelated')
 ON CONFLICT(table_name) DO UPDATE SET classification=excluded.classification;
DO $$ DECLARE t text; op text; clause text; BEGIN
 FOREACH t IN ARRAY ARRAY['payroll_projection_control','payroll_protocol_events','payroll_projection_sources','payroll_projection_changes',
 'payroll_projection_jobs','payroll_projection_generations','payroll_statement_versions','payroll_statement_state',
 'payroll_projection_weeks','payroll_command_receipts','payroll_command_versions','payroll_load_facts','payroll_routing_facts','payroll_cost_months','payroll_source_months','payroll_resource_revisions'] LOOP
  INSERT INTO payroll_projection_sources VALUES(t,'derived');
  INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
   EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',t);
  END IF;
 END LOOP;
 FOR t IN SELECT table_name FROM payroll_projection_sources WHERE classification IN ('financial','display') ORDER BY table_name LOOP
  EXECUTE format('CREATE TRIGGER payroll_protocol_statement BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION guard_payroll_protocol_statement()',t);
  IF payroll_resource_table(t) THEN
   EXECUTE format('CREATE TRIGGER payroll_protocol_row BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_payroll_protocol_row()',t);
  END IF;
  FOREACH op IN ARRAY ARRAY['insert','update','delete','truncate'] LOOP
   clause=CASE op WHEN 'insert' THEN 'REFERENCING NEW TABLE AS changed_rows'
    WHEN 'update' THEN 'REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows'
    WHEN 'delete' THEN 'REFERENCING OLD TABLE AS previous_rows' ELSE '' END;
   EXECUTE format('CREATE TRIGGER %I AFTER %s ON %I %s FOR EACH STATEMENT EXECUTE FUNCTION capture_payroll_projection_change()',
    'payroll_projection_'||op,upper(op),t,clause);
  END LOOP;
 END LOOP;
END $$;
-- The separately installed reconciliation portal retains its restricted login.
-- Grant only the new capture/protocol objects its existing source writes need.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='fleet_review_portal') THEN
  GRANT SELECT ON payroll_projection_control TO fleet_review_portal;
  GRANT INSERT ON payroll_projection_changes TO fleet_review_portal;
  GRANT USAGE ON SEQUENCE payroll_projection_changes_id_seq TO fleet_review_portal;
  GRANT SELECT,INSERT,UPDATE ON payroll_resource_revisions TO fleet_review_portal;
 END IF;
END $$;
COMMIT;
