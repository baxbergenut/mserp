BEGIN;
-- Operational work orders remain independent of service completion and invoices.
ALTER TABLE maintenance_records
 ADD COLUMN work_status TEXT NOT NULL DEFAULT '' CHECK (work_status IN ('','open','scheduled','in_progress','completed','closed','canceled','needs_review','voided')),
 ADD COLUMN dispatch_status TEXT NOT NULL DEFAULT '' CHECK (dispatch_status IN ('','shop','out_of_service','hold')),
 ADD COLUMN waiting_on TEXT NOT NULL DEFAULT '' CHECK (waiting_on IN ('','shop','diagnosis','approval','parts','pickup')),
 ADD COLUMN scheduled_for DATE,
 ADD COLUMN expected_ready DATE,
 ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('normal','high','urgent')),
 ADD COLUMN no_charge BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN payment_confirmation JSONB CHECK(payment_confirmation IS NULL OR jsonb_typeof(payment_confirmation)='object'),
 ADD COLUMN cancel_reason TEXT NOT NULL DEFAULT '' CHECK (length(cancel_reason)<=1000),
 ADD CONSTRAINT maintenance_workflow_completion CHECK (work_status NOT IN ('completed','closed') OR status='completed'),
 ADD CONSTRAINT maintenance_workflow_charge CHECK (NOT no_charge OR expense_id IS NULL);
CREATE INDEX maintenance_dispatch_holds ON maintenance_records(truck_id) WHERE dispatch_status<>'';

-- Finishing the payment step confirms the existing invoice. It neither issues
-- money nor changes driver/owner settlement recovery.
CREATE FUNCTION maintenance_payment_check(record UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE m maintenance_records; e expenses;
BEGIN
 SELECT * INTO m FROM maintenance_records WHERE id=record;
 IF NOT FOUND THEN RETURN; END IF;
 IF m.payment_confirmation IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=m.expense_id;
  IF NOT FOUND OR m.status='voided'
   OR m.payment_confirmation->>'expenseId' IS DISTINCT FROM e.id::text
   OR (m.payment_confirmation->>'amount')::numeric IS DISTINCT FROM e.amount
   OR coalesce(m.payment_confirmation->>'method','') IS DISTINCT FROM coalesce(e.payment_type,'')
   OR coalesce(m.payment_confirmation->>'reference','') IS DISTINCT FROM coalesce(e.reference_number,'')
   OR coalesce(m.payment_confirmation->>'paidBy','') IS DISTINCT FROM coalesce(e.paid_by,'') THEN
   RAISE EXCEPTION 'Confirmed payment retains its invoice, amount and payment details' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
 END IF;
 IF m.work_status='closed' AND (m.dispatch_status<>'' OR (m.expense_id IS NULL AND NOT m.no_charge) OR (m.expense_id IS NOT NULL AND m.payment_confirmation IS NULL)) THEN
  RAISE EXCEPTION 'Finish the work, release its hold and confirm invoice payment before finishing' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;
CREATE FUNCTION maintenance_payment_record_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM maintenance_payment_check(NEW.id); RETURN NULL; END $$;
CREATE CONSTRAINT TRIGGER maintenance_payment_record_guard AFTER INSERT OR UPDATE ON maintenance_records
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION maintenance_payment_record_guard();
CREATE FUNCTION maintenance_payment_expense_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE record UUID;
BEGIN
 FOR record IN SELECT id FROM maintenance_records WHERE expense_id=NEW.id LOOP PERFORM maintenance_payment_check(record); END LOOP;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER maintenance_payment_expense_guard AFTER UPDATE ON expenses
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION maintenance_payment_expense_guard();

ALTER TABLE trucks DROP CONSTRAINT trucks_status_check;
ALTER TABLE trucks ADD CONSTRAINT trucks_status_check CHECK (status IN ('available','assigned','maintenance','out_of_service','hold'));

-- Preserve manually recorded restrictions and every independent work-order hold.
-- Assignment and payment never participate in the release decision.
CREATE FUNCTION fleet_truck_operating_status(truck UUID) RETURNS TEXT LANGUAGE sql STABLE AS $$
 SELECT CASE
 WHEN t.status='out_of_service' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='out_of_service') THEN 'out_of_service'
 WHEN t.status='hold' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='hold') THEN 'hold'
 WHEN t.status='maintenance' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='shop') THEN 'shop'
 ELSE 'ready' END FROM trucks t WHERE t.id=truck
$$;
CREATE TRIGGER maintenance_status_board_changed AFTER INSERT OR UPDATE OF dispatch_status,truck_id ON maintenance_records
 FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change();
COMMIT;
