BEGIN;
CREATE TABLE samsara_telemetry (
 vin text PRIMARY KEY CHECK(vin ~ '^[A-HJ-NPR-Z0-9]{17}$'),
 vehicle_id text NOT NULL,
 unit_number text NOT NULL DEFAULT '',
 available boolean NOT NULL DEFAULT true,
 latitude double precision CHECK(latitude BETWEEN -90 AND 90),
 longitude double precision CHECK(longitude BETWEEN -180 AND 180),
 heading double precision CHECK(heading>=0 AND heading<360),
 speed_mph double precision CHECK(speed_mph BETWEEN 0 AND 200),
 gps_at timestamptz,
 mileage integer CHECK(mileage BETWEEN 0 AND 2000000000),
 odometer_at timestamptz,
 fetched_at timestamptz NOT NULL,
 CHECK((latitude IS NULL)=(longitude IS NULL)),
 CHECK((latitude IS NULL)=(gps_at IS NULL)),
 CHECK((mileage IS NULL)=(odometer_at IS NULL))
);
CREATE TABLE samsara_tracking (
 vin text NOT NULL REFERENCES samsara_telemetry(vin) ON DELETE CASCADE,
 reported_at timestamptz NOT NULL,
 latitude double precision NOT NULL CHECK(latitude BETWEEN -90 AND 90),
 longitude double precision NOT NULL CHECK(longitude BETWEEN -180 AND 180),
 speed_mph double precision CHECK(speed_mph BETWEEN 0 AND 200),
 PRIMARY KEY(vin,reported_at)
);
CREATE TABLE samsara_sync_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 last_attempt_at timestamptz,
 last_success_at timestamptz,
 last_error text NOT NULL DEFAULT '',
 fetched integer NOT NULL DEFAULT 0,
 saved integer NOT NULL DEFAULT 0,
 ambiguous_vins text[] NOT NULL DEFAULT '{}',
 invalid_count integer NOT NULL DEFAULT 0
);
CREATE TRIGGER status_board_changed AFTER INSERT OR UPDATE OR DELETE ON samsara_telemetry FOR EACH STATEMENT EXECUTE FUNCTION notify_status_board_change();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE samsara_telemetry OWNER TO mserp_app;
  ALTER TABLE samsara_tracking OWNER TO mserp_app;
  ALTER TABLE samsara_sync_state OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
