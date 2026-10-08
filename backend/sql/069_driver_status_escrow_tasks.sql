BEGIN;

ALTER TABLE drivers ADD COLUMN status text NOT NULL DEFAULT 'active';
ALTER TABLE drivers ADD COLUMN termination_date date;
ALTER TABLE drivers ADD COLUMN termination_id uuid;
UPDATE drivers d SET status='terminated', termination_id=gen_random_uuid(),
 termination_date=coalesce((SELECT min(t.termination_date) FROM fleetscope_driver_terminations t WHERE t.driver_id=d.id),(now() AT TIME ZONE 'America/New_York')::date-30)
 WHERE NOT active;
ALTER TABLE drivers ADD CONSTRAINT drivers_status_check CHECK(status IN ('active','vacation','home','terminated'));

-- Older clients still write active; preserve named active statuses on those writes.
CREATE FUNCTION sync_driver_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NOT NEW.active AND NEW.status='active' THEN NEW.status='terminated'; END IF;
 ELSIF NEW.status IS NOT DISTINCT FROM OLD.status AND NEW.active IS DISTINCT FROM OLD.active THEN
  NEW.status=CASE WHEN NEW.active THEN 'active' ELSE 'terminated' END;
 END IF;
 NEW.active=NEW.status<>'terminated';
 IF NEW.status='terminated' THEN
  IF TG_OP='INSERT' THEN
   NEW.termination_id=coalesce(NEW.termination_id,gen_random_uuid());
  ELSIF OLD.status<>'terminated' THEN
   NEW.termination_id=gen_random_uuid();
  END IF;
  NEW.termination_date=coalesce(NEW.termination_date,(now() AT TIME ZONE 'America/New_York')::date);
 ELSE
  NEW.termination_date=NULL;
  NEW.termination_id=NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_status BEFORE INSERT OR UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION sync_driver_status();

ALTER TABLE system_task_assignments DROP CONSTRAINT system_task_assignments_kind_check;
ALTER TABLE system_task_assignments ADD CONSTRAINT system_task_assignments_kind_check CHECK(kind IN ('driver_onboarding','driver_offboarding','relay_review','escrow_release'));
ALTER TABLE system_task_assignments ADD COLUMN assignee_ids uuid[] NOT NULL DEFAULT '{}';
INSERT INTO system_task_assignments(kind) VALUES('escrow_release');

CREATE TABLE escrow_release_reviews (
 id uuid PRIMARY KEY,
 driver_id uuid REFERENCES drivers(id) ON DELETE SET NULL,
 driver_name text NOT NULL,
 termination_date date NOT NULL,
 due_date date GENERATED ALWAYS AS (termination_date+30) STORED,
 decision text CHECK(decision IN ('released','partially_released','kept')),
 reason text NOT NULL DEFAULT '',
 balance_snapshot jsonb,
 completed_by uuid REFERENCES app_users(id) ON DELETE SET NULL,
 completed_at timestamptz,
 CHECK ((decision IS NULL AND completed_at IS NULL) OR
        (decision IS NOT NULL AND completed_at IS NOT NULL AND length(btrim(reason)) BETWEEN 1 AND 5000))
);
CREATE INDEX escrow_release_reviews_due_idx ON escrow_release_reviews(due_date) WHERE completed_at IS NULL;
CREATE FUNCTION sync_escrow_release_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='terminated' THEN
  INSERT INTO escrow_release_reviews(id,driver_id,driver_name,termination_date)
  VALUES(NEW.termination_id,NEW.id,NEW.full_name,NEW.termination_date)
  ON CONFLICT(id) DO UPDATE SET driver_name=excluded.driver_name,termination_date=excluded.termination_date
  WHERE escrow_release_reviews.completed_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER driver_escrow_review AFTER INSERT OR UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION sync_escrow_release_review();
INSERT INTO escrow_release_reviews(id,driver_id,driver_name,termination_date)
 SELECT termination_id,id,full_name,termination_date FROM drivers WHERE status='terminated';

CREATE TABLE driver_escrow_opening_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 escrow_id uuid NOT NULL REFERENCES driver_escrows(id),
 actor_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 before_amount numeric(14,2) NOT NULL,
 after_amount numeric(14,2) NOT NULL,
 reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE escrow_release_reviews OWNER TO mserp_app;
  ALTER TABLE driver_escrow_opening_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
