BEGIN;
CREATE TABLE weighmytruck_memberships (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 driver_id uuid UNIQUE REFERENCES drivers(id) ON DELETE SET NULL,
 email text NOT NULL UNIQUE CHECK (email = lower(btrim(email)) AND email <> ''),
 first_name text NOT NULL,
 last_name text NOT NULL,
 phone text NOT NULL DEFAULT '',
 driver_code text NOT NULL DEFAULT '',
 enrolled boolean NOT NULL DEFAULT false,
 state text NOT NULL DEFAULT 'confirmed' CHECK(state IN ('confirmed','pending','review')),
 version integer NOT NULL DEFAULT 1,
 last_error text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE weighmytruck_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 membership_id uuid NOT NULL REFERENCES weighmytruck_memberships(id),
 actor_id text NOT NULL,
 action text NOT NULL,
 detail text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE weighmytruck_imports (
 digest text PRIMARY KEY,
 row_count integer NOT NULL,
 imported_at timestamptz NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE weighmytruck_memberships OWNER TO mserp_app;
  ALTER TABLE weighmytruck_events OWNER TO mserp_app;
  ALTER TABLE weighmytruck_imports OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
