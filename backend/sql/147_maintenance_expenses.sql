BEGIN;

-- Keep service/odometer/program evidence, but use the ordinary expense editor.
SELECT fleet_status_lock();
ALTER TABLE maintenance_records ADD COLUMN expense_entry boolean NOT NULL DEFAULT false;
ALTER TABLE maintenance_records DROP CONSTRAINT maintenance_workflow_charge;
ALTER TABLE maintenance_records ADD CONSTRAINT maintenance_workflow_charge CHECK (expense_entry OR NOT no_charge OR expense_id IS NULL);

ALTER FUNCTION maintenance_validate_record(uuid) RENAME TO maintenance_validate_work_order;
CREATE FUNCTION maintenance_validate_record(record_id uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE m maintenance_records;
BEGIN
 SELECT * INTO m FROM maintenance_records WHERE id=record_id;
 IF NOT FOUND THEN RETURN; END IF;
 IF NOT m.expense_entry THEN PERFORM maintenance_validate_work_order(record_id); RETURN; END IF;
 IF m.status='voided' THEN RETURN; END IF;
 IF m.expense_id IS NULL OR NOT EXISTS(SELECT 1 FROM expenses e JOIN maintenance_configuration c ON c.category_id=e.category_id WHERE e.id=m.expense_id AND e.truck_id IS NOT DISTINCT FROM m.truck_id) THEN
  RAISE EXCEPTION 'Maintenance details must retain their expense and linked truck' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION maintenance_legacy_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.truck_id IS DISTINCT FROM OLD.truck_id OR NEW.cost_breakdown IS DISTINCT FROM OLD.cost_breakdown OR NEW.expense_type IS DISTINCT FROM OLD.expense_type THEN
  UPDATE maintenance_records SET truck_id=NEW.truck_id,services=maintenance_legacy_services(NEW.expense_type,NEW.cost_breakdown),version=version+1,updated_at=now() WHERE expense_id=NEW.id AND status='needs_review' AND NOT expense_entry;
 END IF;
 RETURN NEW;
END $$;

-- Only the retired workflow completion requirement changes. Original payment
-- confirmations and explicit correction snapshots still validate the invoice.
CREATE OR REPLACE FUNCTION maintenance_payment_check(record uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE m maintenance_records; e expenses;
BEGIN
 SELECT * INTO m FROM maintenance_records WHERE id=record;
 IF NOT FOUND THEN RETURN; END IF;
 IF m.payment_confirmation IS NOT NULL THEN
  IF m.status='voided' AND m.expense_id IS NULL THEN
   SELECT * INTO e FROM jsonb_populate_record(NULL::expenses,m.voided_expense);
  ELSE SELECT * INTO e FROM expenses WHERE id=m.expense_id; END IF;
  IF e.id IS NULL
   OR coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'expenseId',m.payment_confirmation->>'expenseId') IS DISTINCT FROM e.id::text
   OR (coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'amount',m.payment_confirmation->>'amount'))::numeric IS DISTINCT FROM e.amount
   OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'method',m.payment_confirmation->>'method'),'') IS DISTINCT FROM coalesce(e.payment_type,'')
   OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'reference',m.payment_confirmation->>'reference'),'') IS DISTINCT FROM coalesce(e.reference_number,'')
   OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'paidBy',m.payment_confirmation->>'paidBy'),'') IS DISTINCT FROM coalesce(e.paid_by,'') THEN
   RAISE EXCEPTION 'Confirmed payment retains its invoice, amount and payment details' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
 END IF;
 IF NOT m.expense_entry AND m.work_status='closed' AND (m.dispatch_status<>'' OR (m.expense_id IS NULL AND NOT m.no_charge) OR (m.expense_id IS NOT NULL AND m.payment_confirmation IS NULL)) THEN
  RAISE EXCEPTION 'Finish the work, release its hold and confirm invoice payment before finishing' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;

UPDATE expense_settings SET name='Maintenance',version=version+1 WHERE id=(SELECT category_id FROM maintenance_configuration);
INSERT INTO expense_settings(kind,category_id,name,active)
SELECT 'name',c.category_id,t.name,t.active FROM maintenance_configuration c CROSS JOIN maintenance_types t
WHERE NOT EXISTS(SELECT 1 FROM expense_settings s WHERE s.kind='name' AND s.category_id=c.category_id AND lower(btrim(s.name))=lower(btrim(t.name)));

-- Expense types own the names and archive state. Stable links retain program
-- associations when a type is renamed in Expenses settings.
ALTER TABLE maintenance_types ADD COLUMN expense_type_id uuid UNIQUE REFERENCES expense_settings(id) ON DELETE RESTRICT;
UPDATE maintenance_types t SET expense_type_id=s.id FROM expense_settings s,maintenance_configuration c WHERE s.kind='name' AND s.category_id=c.category_id AND lower(btrim(s.name))=lower(btrim(t.name));
INSERT INTO maintenance_types(code,name,active,expense_type_id,program_id)
SELECT 'expense_'||replace(s.id::text,'-',''),s.name,s.active,s.id,CASE WHEN lower(btrim(s.name))='pm service' THEN (SELECT program_id FROM maintenance_types WHERE code='preventive') ELSE NULL END FROM expense_settings s JOIN maintenance_configuration c ON c.category_id=s.category_id WHERE s.kind='name' AND NOT EXISTS(SELECT 1 FROM maintenance_types t WHERE t.expense_type_id=s.id);
CREATE FUNCTION sync_maintenance_expense_type() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='name' AND EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=NEW.category_id) THEN
  UPDATE maintenance_types SET name=NEW.name,active=NEW.active,version=version+1 WHERE expense_type_id=NEW.id;
  IF NOT FOUND THEN
   INSERT INTO maintenance_types(code,name,active,expense_type_id) VALUES('expense_'||replace(NEW.id::text,'-',''),NEW.name,NEW.active,NEW.id);
  END IF;
 ELSE UPDATE maintenance_types SET active=false,version=version+1 WHERE expense_type_id=NEW.id AND active;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER maintenance_expense_type_sync AFTER INSERT OR UPDATE OF name,active,category_id ON expense_settings FOR EACH ROW EXECUTE FUNCTION sync_maintenance_expense_type();

-- A truck's effective restriction survives retirement of its work-order holds.
-- Truck profile/Status Board condition controls remain the explicit release path.
CREATE TEMP TABLE maintenance_expense_before ON COMMIT DROP AS SELECT id,to_jsonb(m) AS value FROM maintenance_records m WHERE status<>'voided';
UPDATE trucks t SET status=CASE fleet_truck_operating_status(t.id) WHEN 'shop' THEN 'maintenance' ELSE fleet_truck_operating_status(t.id) END,updated_at=now()
WHERE EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status<>'');

-- Missing invoices stay unknown, and never create a collection or a new export.
-- Disable only insertion-side compatibility/export/history triggers in this
-- transaction; all existing financial protection triggers continue to run.
ALTER TABLE expenses DISABLE TRIGGER maintenance_expense_capture;
ALTER TABLE expenses DISABLE TRIGGER expense_sheet_export_capture;
ALTER TABLE expenses DISABLE TRIGGER expense_sheet_history_settled;
CREATE TEMP TABLE maintenance_expense_links ON COMMIT DROP AS SELECT id AS record_id,gen_random_uuid() AS expense_id FROM maintenance_records WHERE expense_id IS NULL AND status<>'voided';
INSERT INTO expenses(id,category_id,category,expense_date,truck_id,driver_id,unit_number,driver_name,expense_type,description,created_by,created_by_name,created_at,updated_at)
SELECT l.expense_id,c.category_id,'Maintenance',m.service_date,m.truck_id,m.driver_id,nullif(m.source_unit_number,''),nullif(m.driver_name,''),
 left(coalesce(nullif((SELECT string_agg(s->>'name',', ' ORDER BY ord) FROM jsonb_array_elements(m.services) WITH ORDINALITY a(s,ord)),''),'Maintenance'),100),nullif(m.notes,''),m.created_by,nullif(m.created_by_name,''),m.created_at,m.updated_at
FROM maintenance_expense_links l JOIN maintenance_records m ON m.id=l.record_id CROSS JOIN maintenance_configuration c;
UPDATE maintenance_records m SET expense_id=l.expense_id,expense_entry=true FROM maintenance_expense_links l WHERE m.id=l.record_id;
UPDATE maintenance_records SET expense_entry=true,dispatch_status='',version=version+1,updated_at=now(),updated_by_name='Maintenance expense migration' WHERE status<>'voided';
SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE expenses ENABLE TRIGGER maintenance_expense_capture;
ALTER TABLE expenses ENABLE TRIGGER expense_sheet_export_capture;
ALTER TABLE expenses ENABLE TRIGGER expense_sheet_history_settled;

INSERT INTO expense_documents(expense_id,file_name,content_type,size_bytes,sha256,data,uploaded_by,created_at)
SELECT m.expense_id,d.file_name,d.content_type,d.size_bytes,d.sha256,d.data,d.uploaded_by,d.created_at FROM maintenance_documents d JOIN maintenance_records m ON m.id=d.record_id WHERE m.expense_id IS NOT NULL
ON CONFLICT(expense_id,sha256,file_name) DO NOTHING;
INSERT INTO maintenance_changes(record_id,actor_name,before_value,after_value)
SELECT m.id,'Maintenance expense migration',b.value,to_jsonb(m) FROM maintenance_records m JOIN maintenance_expense_before b ON b.id=m.id;

COMMIT;
