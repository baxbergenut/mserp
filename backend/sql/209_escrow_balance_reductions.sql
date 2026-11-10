BEGIN;

-- Balance reductions consume funds on the same schedule as releases and use
-- the same funding and audit triggers, without creating a payroll credit.
ALTER TABLE driver_escrow_releases
 ADD COLUMN correction_reason text,
 ADD CONSTRAINT escrow_balance_reduction_check CHECK (
    correction_reason IS NULL OR
    (char_length(btrim(correction_reason)) BETWEEN 1 AND 5000 AND payroll_return IS NULL
     AND collection_stopped IS NULL AND NOT cancelled)
 );

CREATE FUNCTION protect_escrow_balance_reduction() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.correction_reason IS NOT NULL OR NEW.correction_reason IS NOT NULL THEN
    RAISE EXCEPTION 'Escrow balance reductions cannot be edited' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER escrow_balance_reduction_guard
 BEFORE UPDATE OF escrow_id,week_start,amount,cancelled,correction_reason ON driver_escrow_releases
 FOR EACH ROW EXECUTE FUNCTION protect_escrow_balance_reduction();

COMMIT;
