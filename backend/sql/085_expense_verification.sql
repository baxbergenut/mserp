BEGIN;

CREATE TABLE expense_verifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    expense_id uuid NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    verifier_role text NOT NULL CHECK (verifier_role IN ('admin', 'accountant')),
    user_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
    user_name text NOT NULL DEFAULT '',
    verified_at timestamptz,
    revoked_at timestamptz,
    revoked_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
    revoked_by_name text
);
CREATE UNIQUE INDEX expense_verifications_active ON expense_verifications(expense_id, verifier_role) WHERE revoked_at IS NULL;

-- Historical checks have no recorded verifier or timestamp. Preserve them
-- without attributing the approval to the migration operator.
INSERT INTO expense_verifications(expense_id, verifier_role)
SELECT id, 'admin' FROM expenses WHERE manager_verified;
INSERT INTO expense_verifications(expense_id, verifier_role)
SELECT id, 'accountant' FROM expenses WHERE accounting_verified;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE expense_verifications OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
