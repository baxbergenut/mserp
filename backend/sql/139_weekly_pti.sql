BEGIN;

CREATE TABLE weekly_pti_weeks (
 week_start date PRIMARY KEY CHECK(extract(isodow FROM week_start)=1),
 created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL,
 late_snapshot boolean NOT NULL
);
CREATE TABLE weekly_pti_rows (
 week_start date NOT NULL REFERENCES weekly_pti_weeks(week_start), truck_id uuid NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), data jsonb NOT NULL,
 PRIMARY KEY(week_start,truck_id),
 CHECK(data->>'eligibility' IN ('required','exempt','unknown')),
 CHECK(data->>'review' IN ('pending','checked','issues')),
 CHECK((data->>'received')::boolean OR data->>'review'='pending')
);
CREATE TABLE weekly_pti_events (
 id uuid PRIMARY KEY, request_hash text NOT NULL, week_start date NOT NULL, truck_id uuid NOT NULL,
 before_data jsonb NOT NULL, after_data jsonb NOT NULL,
 actor_id uuid NOT NULL, actor_name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX weekly_pti_events_record ON weekly_pti_events(week_start,truck_id,created_at DESC);
CREATE FUNCTION protect_weekly_pti_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'PTI history is append-only';
END $$;
CREATE TRIGGER weekly_pti_events_immutable BEFORE UPDATE OR DELETE ON weekly_pti_events
 FOR EACH ROW EXECUTE FUNCTION protect_weekly_pti_history();
CREATE TRIGGER weekly_pti_weeks_immutable BEFORE UPDATE OR DELETE ON weekly_pti_weeks
 FOR EACH ROW EXECUTE FUNCTION protect_weekly_pti_history();

-- Start with the same readers and editors as the existing fleet profiles.
UPDATE app_roles SET permissions=permissions||ARRAY['pti.read']::text[],version=version+1
 WHERE NOT system_role AND ARRAY['drivers.read','trucks.read']::text[] <@ permissions AND NOT 'pti.read'=ANY(permissions);
UPDATE app_roles SET permissions=permissions||ARRAY['pti.edit']::text[],version=version+1
 WHERE NOT system_role AND ARRAY['pti.read','trucks.edit']::text[] <@ permissions AND NOT 'pti.edit'=ANY(permissions);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE weekly_pti_weeks OWNER TO mserp_app;
  ALTER TABLE weekly_pti_rows OWNER TO mserp_app;
  ALTER TABLE weekly_pti_events OWNER TO mserp_app;
  ALTER FUNCTION protect_weekly_pti_history() OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
