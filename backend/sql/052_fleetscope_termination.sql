BEGIN;

-- FleetScope termination receipts survive task/driver deletion and late hire delivery.
ALTER TABLE fleetscope_driver_intake ADD COLUMN terminated_at TIMESTAMPTZ;
CREATE TABLE fleetscope_driver_terminations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    fleetscope_driver_id UUID NOT NULL,
    driver_name TEXT NOT NULL,
    termination_date DATE NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    task_id UUID REFERENCES custom_tasks(id) ON DELETE SET NULL,
    UNIQUE(company_id, fleetscope_driver_id)
);
ALTER TABLE fleetscope_webhook_receipts ALTER COLUMN intake_id DROP NOT NULL;
ALTER TABLE fleetscope_webhook_receipts ADD COLUMN termination_id UUID REFERENCES fleetscope_driver_terminations(id);
ALTER TABLE fleetscope_webhook_receipts ADD CONSTRAINT fleetscope_receipt_target
    CHECK (num_nonnulls(intake_id, termination_id) = 1);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE fleetscope_driver_terminations OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
