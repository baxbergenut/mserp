BEGIN;

-- Applicants are expense identities, never operational drivers or payroll debtors.
CREATE TABLE fleetscope_applicants (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 company_id uuid NOT NULL,
 fleetscope_applicant_id uuid NOT NULL,
 fleetscope_driver_id uuid,
 full_name text NOT NULL CHECK(length(btrim(full_name)) BETWEEN 1 AND 200),
 status text NOT NULL CHECK(status IN ('applied','hired','rejected','withdrawn')),
 source_updated_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(company_id,fleetscope_applicant_id),
 UNIQUE(company_id,fleetscope_driver_id),
 CHECK ((status='hired') = (fleetscope_driver_id IS NOT NULL))
);
ALTER TABLE fleetscope_webhook_receipts ADD COLUMN applicant_id uuid REFERENCES fleetscope_applicants(id);
ALTER TABLE fleetscope_webhook_receipts DROP CONSTRAINT fleetscope_receipt_target;
ALTER TABLE fleetscope_webhook_receipts ADD CONSTRAINT fleetscope_receipt_target
 CHECK(num_nonnulls(intake_id,termination_id,applicant_id)=1);
ALTER TABLE expenses ADD COLUMN applicant_id uuid REFERENCES fleetscope_applicants(id);
CREATE INDEX expenses_applicant_idx ON expenses(applicant_id) WHERE applicant_id IS NOT NULL;

-- Resolve through reviewed source UUID links, including hires completed after an expense.
-- Do not rewrite accounting records, payment responsibility, or frozen settlements.
CREATE FUNCTION expense_applicant_driver(applicant uuid) RETURNS uuid LANGUAGE sql STABLE STRICT AS $$
 SELECT d.id FROM fleetscope_applicants a
 JOIN fleetscope_driver_identity i ON i.company_id=a.company_id AND i.fleetscope_driver_id=a.fleetscope_driver_id AND i.confirmed
 JOIN drivers d ON d.id=i.driver_id WHERE a.id=applicant
$$;
CREATE FUNCTION expense_applicant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE applicant_name text; linked_driver uuid;
BEGIN
 IF NEW.applicant_id IS NULL THEN RETURN NEW; END IF;
 SELECT full_name INTO applicant_name FROM fleetscope_applicants WHERE id=NEW.applicant_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'Select an available applicant' USING ERRCODE='23503'; END IF;
 linked_driver := expense_applicant_driver(NEW.applicant_id);
 IF NEW.driver_id IS NOT NULL AND NEW.driver_id IS DISTINCT FROM linked_driver THEN
  RAISE EXCEPTION 'Applicant and driver do not identify the same person' USING ERRCODE='23514',CONSTRAINT='expense_applicant_identity';
 END IF;
 IF NEW.driver_id IS NULL THEN NEW.driver_name := applicant_name; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expenses_000_applicant BEFORE INSERT OR UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION expense_applicant_guard();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  GRANT SELECT,INSERT,UPDATE,DELETE ON fleetscope_applicants TO mserp_app;
 END IF;
END $$;
COMMIT;
