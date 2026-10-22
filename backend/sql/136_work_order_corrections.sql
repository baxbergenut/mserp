BEGIN;

-- Preserve the original payment confirmation while auditing corrected invoice details.
CREATE OR REPLACE FUNCTION maintenance_payment_check(record UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE m maintenance_records; e expenses;
BEGIN
 SELECT * INTO m FROM maintenance_records WHERE id=record;
 IF NOT FOUND THEN RETURN; END IF;
 IF m.payment_confirmation IS NOT NULL THEN
  SELECT * INTO e FROM expenses WHERE id=m.expense_id;
  IF NOT FOUND OR m.status='voided'
   OR coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'expenseId',m.payment_confirmation->>'expenseId') IS DISTINCT FROM e.id::text
   OR (coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'amount',m.payment_confirmation->>'amount'))::numeric IS DISTINCT FROM e.amount
   OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'method',m.payment_confirmation->>'method'),'') IS DISTINCT FROM coalesce(e.payment_type,'')
   OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'reference',m.payment_confirmation->>'reference'),'') IS DISTINCT FROM coalesce(e.reference_number,'')
   OR coalesce(coalesce(m.payment_confirmation->'invoiceCorrections'->-1->>'paidBy',m.payment_confirmation->>'paidBy'),'') IS DISTINCT FROM coalesce(e.paid_by,'') THEN
   RAISE EXCEPTION 'Confirmed payment retains its invoice, amount and payment details' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
  END IF;
 END IF;
 IF m.work_status='closed' AND (m.dispatch_status<>'' OR (m.expense_id IS NULL AND NOT m.no_charge) OR (m.expense_id IS NOT NULL AND m.payment_confirmation IS NULL)) THEN
  RAISE EXCEPTION 'Finish the work, release its hold and confirm invoice payment before finishing' USING ERRCODE='23514',CONSTRAINT='maintenance_work_total';
 END IF;
END $$;

COMMIT;
