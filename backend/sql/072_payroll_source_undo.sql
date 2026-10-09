BEGIN;
CREATE TABLE payroll_source_actions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id text NOT NULL,
 driver_id uuid NOT NULL,
 service_date date NOT NULL,
 slot integer NOT NULL,
 applied_version integer NOT NULL,
 expected_version integer NOT NULL,
 before_values jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 undone_at timestamptz
);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE payroll_source_actions OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
