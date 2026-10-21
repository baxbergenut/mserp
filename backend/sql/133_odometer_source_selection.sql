BEGIN;

-- Raw latest provider observations remain visible even when plausibility checks
-- prevent them from replacing the accepted operational odometer.
ALTER TABLE maintenance_telemetry ADD COLUMN observed_mileage integer CHECK(observed_mileage BETWEEN 0 AND 2000000000);
ALTER TABLE maintenance_telemetry ADD COLUMN observed_at timestamptz;
UPDATE maintenance_telemetry SET observed_mileage=mileage,observed_at=reported_at;
ALTER TABLE maintenance_telemetry ADD CONSTRAINT maintenance_telemetry_observed_pair CHECK((observed_mileage IS NULL)=(observed_at IS NULL));
ALTER TABLE samsara_telemetry ADD COLUMN observed_mileage integer CHECK(observed_mileage BETWEEN 0 AND 2000000000);
ALTER TABLE samsara_telemetry ADD COLUMN observed_at timestamptz;
UPDATE samsara_telemetry SET observed_mileage=mileage,observed_at=odometer_at;
ALTER TABLE samsara_telemetry ADD CONSTRAINT samsara_telemetry_observed_pair CHECK((observed_mileage IS NULL)=(observed_at IS NULL));

ALTER TABLE maintenance_odometer_readings ADD COLUMN observed_at timestamptz;
ALTER TABLE maintenance_odometer_readings ADD COLUMN note text NOT NULL DEFAULT '' CHECK(length(note)<=500);

CREATE TABLE maintenance_odometer_sources (
 truck_id uuid PRIMARY KEY REFERENCES trucks(id) ON DELETE CASCADE,
 source text NOT NULL CHECK(source IN ('auto','samsara','five_eld','manual')),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 actor_name text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE maintenance_odometer_sources OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
