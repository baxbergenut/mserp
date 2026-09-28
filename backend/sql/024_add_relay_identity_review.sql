BEGIN;
ALTER TABLE relay_driver_links ALTER COLUMN driver_id DROP NOT NULL;
ALTER TABLE fuel_transactions ALTER COLUMN driver_id DROP NOT NULL;
CREATE INDEX relay_driver_links_pending_idx ON relay_driver_links(created_at, id) WHERE driver_id IS NULL;
CREATE INDEX fuel_transactions_relay_identity_idx ON fuel_transactions(relay_environment, relay_driver_id);
CREATE TABLE relay_identity_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES relay_driver_links(id) ON DELETE RESTRICT,
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    action TEXT NOT NULL CHECK (action IN ('link', 'reject')),
    reviewed_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(identity_id, driver_id, action)
);
COMMIT;

