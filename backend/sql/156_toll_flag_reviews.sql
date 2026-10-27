BEGIN;
CREATE TABLE toll_flag_review_events (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 id uuid NOT NULL UNIQUE,
 toll_id uuid NOT NULL REFERENCES tolls(id) ON DELETE RESTRICT,
 revision text NOT NULL CHECK (length(revision)=64),
 previous_version text NOT NULL,
 reviewed boolean NOT NULL,
 evidence jsonb NOT NULL,
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX toll_flag_review_latest ON toll_flag_review_events(toll_id,sequence DESC);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE toll_flag_review_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
