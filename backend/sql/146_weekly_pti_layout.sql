BEGIN;
CREATE TABLE weekly_pti_layout_state (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), version bigint NOT NULL DEFAULT 1
);
INSERT INTO weekly_pti_layout_state(id) VALUES(true);
CREATE TABLE weekly_pti_layout (
 truck_id uuid PRIMARY KEY REFERENCES trucks(id) ON DELETE CASCADE,
 display_group text NOT NULL CHECK(display_group IN ('company','owner_operator')),
 position integer NOT NULL CHECK(position>=0)
);
CREATE TABLE weekly_pti_layout_events (
 id bigserial PRIMARY KEY, actor_id uuid NOT NULL REFERENCES app_users(id),
 before_layout jsonb NOT NULL, after_layout jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER weekly_pti_layout_events_immutable BEFORE UPDATE OR DELETE ON weekly_pti_layout_events
 FOR EACH ROW EXECUTE FUNCTION protect_weekly_pti_history();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE weekly_pti_layout_state OWNER TO mserp_app;
  ALTER TABLE weekly_pti_layout OWNER TO mserp_app;
  ALTER TABLE weekly_pti_layout_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
