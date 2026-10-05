BEGIN;

-- Current Five ELD telemetry is operational cache data. It is matched to the
-- assigned truck by normalized VIN and never changes fleet assignments.
CREATE TABLE five_eld_locations (
    vin TEXT PRIMARY KEY CHECK (vin ~ '^[A-Z0-9]{17}$'),
    provider_truck_number TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    reported_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    address_updated_at TIMESTAMPTZ,
    address_reported_at TIMESTAMPTZ
);

CREATE TABLE five_eld_sync_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    stale_after_seconds INTEGER NOT NULL DEFAULT 900 CHECK (stale_after_seconds BETWEEN 60 AND 86400),
    unmatched_vins TEXT[] NOT NULL DEFAULT '{}',
    ambiguous_vins TEXT[] NOT NULL DEFAULT '{}',
    invalid_unit_count INTEGER NOT NULL DEFAULT 0 CHECK (invalid_unit_count >= 0)
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE five_eld_locations OWNER TO mserp_app;
        ALTER TABLE five_eld_sync_state OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
