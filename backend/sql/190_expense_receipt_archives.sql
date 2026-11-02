-- Retain immutable financial receipts when their source expense is deleted.
-- Apply with writers quiesced. No existing invoice or payment is changed.
BEGIN;
SELECT pg_advisory_xact_lock(736281941);
ALTER TABLE expense_external_settlements ADD COLUMN expense_snapshot jsonb;
ALTER TABLE expense_balance_corrections ADD COLUMN expense_snapshot jsonb;

CREATE FUNCTION validate_expense_receipt_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.expense_snapshot IS NOT NULL OR NOT EXISTS(SELECT 1 FROM expenses WHERE id=NEW.expense_id) THEN
  RAISE EXCEPTION 'A new receipt requires a live expense' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_external_source_guard BEFORE INSERT ON expense_external_settlements
 FOR EACH ROW EXECUTE FUNCTION validate_expense_receipt_source();
CREATE TRIGGER expense_balance_source_guard BEFORE INSERT ON expense_balance_corrections
 FOR EACH ROW EXECUTE FUNCTION validate_expense_receipt_source();

CREATE FUNCTION protect_archived_expense_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.expense_snapshot IS NULL
 AND current_setting('mserp.deleting_expense',true)=OLD.expense_id::text
 AND (to_jsonb(NEW)-'expense_snapshot')=(to_jsonb(OLD)-'expense_snapshot')
 AND NEW.expense_snapshot=(SELECT to_jsonb(e) FROM expenses e WHERE e.id=OLD.expense_id) THEN
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'Accounting receipts are immutable' USING ERRCODE='23514';
END $$;
DROP TRIGGER expense_external_immutable ON expense_external_settlements;
CREATE TRIGGER expense_external_immutable BEFORE UPDATE OR DELETE ON expense_external_settlements
 FOR EACH ROW EXECUTE FUNCTION protect_archived_expense_receipt();
DROP TRIGGER expense_balance_immutable ON expense_balance_corrections;
CREATE TRIGGER expense_balance_immutable BEFORE UPDATE OR DELETE ON expense_balance_corrections
 FOR EACH ROW EXECUTE FUNCTION protect_archived_expense_receipt();

CREATE FUNCTION archive_deleted_expense_receipts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM set_config('mserp.deleting_expense',OLD.id::text,true);
 UPDATE expense_external_settlements SET expense_snapshot=to_jsonb(OLD) WHERE expense_id=OLD.id;
 UPDATE expense_balance_corrections SET expense_snapshot=to_jsonb(OLD) WHERE expense_id=OLD.id;
 PERFORM set_config('mserp.deleting_expense','',true);
 RETURN OLD;
END $$;
CREATE TRIGGER expense_archive_receipts BEFORE DELETE ON expenses
 FOR EACH ROW EXECUTE FUNCTION archive_deleted_expense_receipts();
-- Insert guards still require a live, valid expense. Deleted sources survive in
-- each receipt's exact snapshot and UUID; financial receipt fields stay immutable.
ALTER TABLE expense_external_settlements DROP CONSTRAINT expense_external_settlements_expense_id_fkey;
ALTER TABLE expense_balance_corrections DROP CONSTRAINT expense_balance_corrections_expense_id_fkey;
COMMIT;
