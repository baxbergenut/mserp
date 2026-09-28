BEGIN;

-- New hires are staged separately until accounting confirms identity and pay.
CREATE TABLE fleetscope_driver_intake (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    fleetscope_driver_id UUID NOT NULL,
    driver_data JSONB NOT NULL,
    normalized_name TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    completed_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    UNIQUE(company_id, fleetscope_driver_id)
);
CREATE INDEX fleetscope_driver_intake_pending_idx
    ON fleetscope_driver_intake(received_at, id) WHERE completed_at IS NULL;

CREATE TABLE fleetscope_webhook_receipts (
    event_id UUID PRIMARY KEY,
    body_sha256 TEXT NOT NULL,
    intake_id UUID NOT NULL REFERENCES fleetscope_driver_intake(id),
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
