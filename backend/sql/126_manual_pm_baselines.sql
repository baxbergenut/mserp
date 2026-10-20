BEGIN;

CREATE TABLE maintenance_pm_baselines (
 truck_id UUID PRIMARY KEY REFERENCES trucks(id) ON DELETE RESTRICT,
 cycle_id UUID NOT NULL DEFAULT gen_random_uuid(),
 service_date DATE NOT NULL,
 odometer INTEGER CHECK(odometer BETWEEN 0 AND 2000000000),
 source_record_id UUID REFERENCES maintenance_records(id) ON DELETE RESTRICT,
 source_version INTEGER NOT NULL,
 version INTEGER NOT NULL DEFAULT 1,
 actor_name TEXT NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE maintenance_pm_baselines OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;
