BEGIN;

-- An outcome belongs to one employment termination, not to a name or a truck
-- forever. Held trucks require explicit human review; retries never deactivate
-- them just because their active driver later leaves.
CREATE TABLE driver_termination_truck_actions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    termination_id uuid NOT NULL,
    -- Historical identity snapshots survive later profile deletion.
    driver_id uuid NOT NULL,
    truck_id uuid NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('inactivated','already_inactive','held')),
    task_id uuid REFERENCES custom_tasks(id) ON DELETE SET NULL,
    before_value jsonb NOT NULL,
    after_value jsonb NOT NULL,
    actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
    source text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (termination_id, truck_id)
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_termination_truck_actions OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
