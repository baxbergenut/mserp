-- Open statements choose deductions; only finalization collects the expense.
-- Apply with writers stopped. Older binaries must not resume after this change.
BEGIN;
SELECT pg_advisory_xact_lock(736281940);
-- Validate the same claims and actual advisory locks using set differences.
-- Array containment and a correlated scan of thousands of historical claims
-- made a single invoice posting quadratic in the driver's assignment history.
CREATE OR REPLACE FUNCTION payroll_assert_resources(required_keys text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE claimed text[]; epoch bigint; protocol text;
BEGIN
 SELECT protocol_epoch,protocol_mode INTO STRICT epoch,protocol FROM payroll_projection_control WHERE singleton;
 IF coalesce(current_setting('mserp.payroll.protocol_epoch',true),'')<>epoch::text THEN
  RAISE EXCEPTION 'Payroll protocol epoch changed' USING ERRCODE='40001';
 END IF;
 SELECT array_agg(value) INTO claimed FROM jsonb_array_elements_text(coalesce(nullif(current_setting('mserp.payroll.resources',true),''),'[]')::jsonb);
 IF coalesce(cardinality(required_keys),0)=0 OR EXISTS(
  SELECT unnest(required_keys) EXCEPT SELECT unnest(claimed)
 ) THEN
  RAISE EXCEPTION 'Payroll resource claim is incomplete' USING ERRCODE='40001';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
  AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=0 AND objid=736281940
  AND objsubid=1 AND (mode='ExclusiveLock' OR (protocol='resources' AND mode='ShareLock')) AND granted) THEN
  RAISE EXCEPTION 'Payroll domain gate is missing' USING ERRCODE='40001';
 END IF;
 IF EXISTS(
  SELECT ((id>>32)&4294967295)::oid,(id&4294967295)::oid
  FROM (SELECT hashtextextended(resource,182736281940) id FROM unnest(required_keys) resource) required
  EXCEPT
  SELECT classid,objid FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
  AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
  AND objsubid=1 AND mode='ExclusiveLock' AND granted
 ) THEN
  RAISE EXCEPTION 'Payroll resource lock is missing' USING ERRCODE='40001';
 END IF;
END $$;
ALTER TABLE payroll_projection_changes ADD COLUMN live_expense boolean NOT NULL DEFAULT false;
-- Capture the payment direction with both routing images. Later source proofs
-- must not infer an old reimbursement's direction from its current record.
DO $$ DECLARE definition text; BEGIN
 definition:=pg_get_functiondef('payroll_projection_route(jsonb)'::regprocedure);
 IF strpos(definition,'''covered_by'',''broker_load_number''')=0 THEN
  RAISE EXCEPTION 'Unexpected expense routing capture contract';
 END IF;
 EXECUTE replace(definition,'''covered_by'',''broker_load_number''','''covered_by'',''broker_load_number'',''payment_type'',''statement_kind''');
END $$;

ALTER TABLE driver_pay_weeks ADD COLUMN expense_choices jsonb NOT NULL DEFAULT '{}'
 CHECK (jsonb_typeof(expense_choices)='object');
ALTER TABLE investor_pay_weeks ADD COLUMN expense_choices jsonb NOT NULL DEFAULT '{}'
 CHECK (jsonb_typeof(expense_choices)='object');
ALTER TABLE expense_payments ADD COLUMN statement_kind text,
 ADD COLUMN statement_id uuid,
 ADD CONSTRAINT expense_payment_statement CHECK (
 (statement_kind IS NULL AND statement_id IS NULL) OR
 (statement_kind IS NOT NULL AND statement_kind IN ('driver','investor') AND statement_id IS NOT NULL));

CREATE FUNCTION payroll_live_driver_expense(route jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT route ? 'payment_type' AND lower(btrim(coalesce(route->>'covered_by','')))='driver'
 AND lower(btrim(coalesce(route->>'payment_type','')))<>'reimburse'
 AND coalesce(route->>'charge_driver_id','')<>''
$$;
CREATE FUNCTION payroll_live_expense_event(table_value text,operation_value text,prior jsonb,following jsonb,revision_only boolean) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT operation_value<>'TRUNCATE' AND prior||following<>'[]'::jsonb
 AND (table_value='expense_payments' OR (table_value='expenses' AND revision_only))
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(prior||following) r WHERE NOT payroll_live_driver_expense(r))
$$;

-- Financial receipts capture their payer at posting time, including deletions.
-- A later source correction cannot change what those captured epochs mean.
CREATE OR REPLACE FUNCTION payroll_projection_row_routes(table_value text,row_value jsonb) RETURNS SETOF jsonb
LANGUAGE sql STABLE AS $$
 SELECT payroll_projection_route(image) FROM (
 SELECT row_value image WHERE table_value NOT IN ('payroll_load_placement_history','gross_board_transfer_history','expense_payments')
 UNION ALL SELECT row_value->'before_value' WHERE table_value IN ('payroll_load_placement_history','gross_board_transfer_history')
 UNION ALL SELECT row_value->'after_value' WHERE table_value IN ('payroll_load_placement_history','gross_board_transfer_history')
 UNION ALL SELECT row_value||jsonb_build_object('covered_by',e.covered_by,'payment_type',e.payment_type,'charge_driver_id',e.charge_driver_id)
 FROM expenses e WHERE table_value='expense_payments' AND e.id::text=row_value->>'expense_id'
 ) images WHERE jsonb_typeof(image)='object'
$$;
CREATE OR REPLACE FUNCTION payroll_row_revision_keys(table_value text,row_value jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN table_value IN ('expenses','expense_payments') AND payroll_live_driver_expense(row_value) THEN
 ARRAY['live-expense:'||CASE WHEN table_value='expenses' THEN row_value->>'id' ELSE row_value->>'expense_id' END]
 WHEN table_value IN ('driver_pay_weeks','payroll_settlements','payroll_settlement_events') THEN
 ARRAY['statement:driver:'||(row_value->>'driver_id')||':'||(row_value->>'week_start')]
 WHEN table_value IN ('investor_pay_weeks','truck_settlement_events') THEN
 ARRAY['statement:investor:'||(row_value->>'truck_id')||':'||(row_value->>'week_start')]
 ELSE payroll_row_resources(table_value,row_value) END
$$;

-- The current statement supplies the reviewed destination. Never infer that an
-- unreferenced historical payment is a draft from today's driver assignment.
CREATE TEMP TABLE expense_draft_cutover ON COMMIT DROP AS
 SELECT p.expense_id,p.week_start,p.amount,c.kind,c.statement_id,c.finalized
 FROM expense_payments p JOIN payroll_statement_current c ON c.week_start=p.week_start
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(c.detail#>'{edits,expenseDeductions}','[]')) item
 WHERE item->>'expenseId'=p.expense_id::text AND coalesce(item->>'source','')=''
 AND ((c.kind='driver' AND p.investor_truck_id IS NULL) OR
      (c.kind='investor' AND p.investor_truck_id=c.statement_id));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM expense_draft_cutover GROUP BY expense_id,week_start HAVING count(*)<>1) THEN
  RAISE EXCEPTION 'An expense allocation has ambiguous statement ownership';
 END IF;
 IF EXISTS(SELECT 1 FROM expense_draft_cutover x WHERE NOT x.finalized AND
  ((x.kind='driver' AND EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id=x.statement_id AND s.week_start=x.week_start AND s.finalized)) OR
   (x.kind='investor' AND EXISTS(SELECT 1 FROM investor_pay_weeks s WHERE s.truck_id=x.statement_id AND s.week_start=x.week_start AND s.finalized)))) THEN
  RAISE EXCEPTION 'Current expense statement disagrees with its finalized ledger';
 END IF;
END $$;

-- Retained posted rows stay untouched. Draft rows become choices on their own
-- statement, preserving explicit zero deferrals and exact selected amounts.
INSERT INTO driver_pay_weeks(driver_id,week_start,expense_choices)
 SELECT statement_id,week_start,jsonb_object_agg(expense_id::text,
 jsonb_build_object('mode',CASE WHEN amount=0 THEN 'skip' ELSE 'fixed' END,'amount',amount::text))
 FROM expense_draft_cutover WHERE NOT finalized AND kind='driver' GROUP BY statement_id,week_start
 ON CONFLICT(driver_id,week_start) DO UPDATE SET expense_choices=excluded.expense_choices;
UPDATE investor_pay_weeks w SET expense_choices=x.choices FROM (
 SELECT statement_id,week_start,jsonb_object_agg(expense_id::text,
 jsonb_build_object('mode',CASE WHEN amount=0 THEN 'skip' ELSE 'fixed' END,'amount',amount::text)) choices
 FROM expense_draft_cutover WHERE NOT finalized AND kind='investor' GROUP BY statement_id,week_start
) x WHERE w.truck_id=x.statement_id AND w.week_start=x.week_start AND NOT w.finalized;
DELETE FROM expense_payments p USING expense_draft_cutover x
 WHERE NOT x.finalized AND p.expense_id=x.expense_id AND p.week_start=x.week_start;
COMMIT;
