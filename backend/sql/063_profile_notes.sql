BEGIN;

-- Existing freeform notes stay on profiles as undated legacy context.
CREATE TABLE profile_notes (
    id uuid PRIMARY KEY,
    driver_id uuid REFERENCES drivers(id) ON DELETE CASCADE,
    truck_id uuid REFERENCES trucks(id) ON DELETE CASCADE,
    body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 5000),
    actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
    actor_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(driver_id, truck_id) = 1)
);
CREATE INDEX profile_notes_driver ON profile_notes(driver_id, created_at DESC);
CREATE INDEX profile_notes_truck ON profile_notes(truck_id, created_at DESC);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE profile_notes OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
