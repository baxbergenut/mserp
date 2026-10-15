BEGIN;
CREATE TABLE maintenance_telemetry (
 truck_id uuid PRIMARY KEY REFERENCES trucks(id) ON DELETE CASCADE,
 vin text NOT NULL,
 mileage integer CHECK(mileage>=0),
 reported_at timestamptz,
 checked_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((mileage IS NULL)=(reported_at IS NULL))
);
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN GRANT SELECT,INSERT,UPDATE,DELETE ON maintenance_telemetry TO mserp_app; END IF; END $$;
-- Preserve category identity, permissions and financial snapshots.
UPDATE expense_settings SET name='Work orders',version=version+1
WHERE id=(SELECT category_id FROM maintenance_configuration) AND name='Maintenance'
AND NOT EXISTS(SELECT 1 FROM expense_settings WHERE kind='category' AND lower(name)='work orders');
COMMIT;
