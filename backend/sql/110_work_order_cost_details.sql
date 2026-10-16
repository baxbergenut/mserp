BEGIN;

-- Separate the work classification from freely named invoice cost rows.
-- No existing service, invoice, payment or approval records are rewritten.
CREATE OR REPLACE FUNCTION maintenance_validate_record(record_id UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r maintenance_records; e expenses; item JSONB; total NUMERIC:=0; priced INTEGER:=0; n INTEGER:=0;
BEGIN
 SELECT * INTO r FROM maintenance_records WHERE id=record_id;
 IF NOT FOUND OR r.status='voided' THEN RETURN; END IF;
 IF r.expense_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM expenses ledger JOIN maintenance_configuration c ON c.category_id=ledger.category_id WHERE ledger.id=r.expense_id) THEN
  RAISE EXCEPTION 'Maintenance invoices retain their classification' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.status='needs_review' THEN RETURN; END IF;
 IF jsonb_array_length(r.services) NOT BETWEEN 1 AND 100 THEN
  RAISE EXCEPTION 'Add completed work items' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(r.services) LOOP
  n:=n+1;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
   OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
   OR NOT EXISTS(SELECT 1 FROM maintenance_types WHERE code=item->>'typeCode')
   OR coalesce(item->>'trackingCode','') NOT IN ('','pm','oil')
   OR (item->>'trackingCode'='pm' AND item->>'typeCode'<>'preventive') OR (item->>'trackingCode'='oil' AND item->>'typeCode'<>'oil') THEN
   RAISE EXCEPTION 'Invalid maintenance work' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  IF item->>'amount' IS NOT NULL THEN
   IF (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$' THEN
    RAISE EXCEPTION 'Invalid maintenance cost' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
   END IF;
   priced:=priced+1; total:=total+(item->>'amount')::numeric;
  END IF;
 END LOOP;
 IF priced<>0 AND priced<>n THEN
  RAISE EXCEPTION 'Price all work items or leave all unallocated' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
 IF r.expense_id IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=r.expense_id;
  IF e.truck_id IS DISTINCT FROM r.truck_id OR NOT EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id)
   OR (priced>0 AND total<>coalesce(e.base_amount,e.amount)) THEN
   RAISE EXCEPTION 'Maintenance work must match its financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
  -- Expense cost rows are independent invoice detail. Their exact base-amount
  -- sum is enforced by expense_cost_breakdown_total; typed work retains its own total.
 ELSIF priced>0 AND total<>0 THEN
  RAISE EXCEPTION 'Paid work needs a financial record' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;

COMMIT;
