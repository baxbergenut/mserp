BEGIN;

ALTER TABLE truck_driver_assignments ADD COLUMN source TEXT NOT NULL DEFAULT '';

CREATE TABLE driver_dispatcher_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    dispatcher_id UUID REFERENCES dispatchers(id) ON DELETE SET NULL,
    dispatcher_name TEXT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unassigned_at TIMESTAMPTZ,
    start_known BOOLEAN NOT NULL DEFAULT true,
    source TEXT NOT NULL DEFAULT '',
    CHECK (unassigned_at IS NULL OR unassigned_at >= assigned_at)
);
CREATE UNIQUE INDEX driver_dispatcher_assignments_current_idx
    ON driver_dispatcher_assignments(driver_id) WHERE unassigned_at IS NULL;
CREATE INDEX driver_dispatcher_assignments_history_idx
    ON driver_dispatcher_assignments(driver_id, assigned_at DESC);

-- Existing links have an observation date, not a known historical start date.
LOCK TABLE drivers IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,start_known,source)
SELECT d.id,d.dispatcher_id,coalesce(dp.full_name,'Unassigned'),false,'Existing assignment when history tracking started'
FROM drivers d LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id;

CREATE FUNCTION record_driver_dispatcher_assignment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
BEGIN
    IF TG_OP='UPDATE' AND NEW.dispatcher_id IS NOT DISTINCT FROM OLD.dispatcher_id THEN
        RETURN NEW;
    END IF;
    UPDATE driver_dispatcher_assignments SET unassigned_at=changed_at
    WHERE driver_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,source)
    VALUES(NEW.id,NEW.dispatcher_id,
        coalesce((SELECT full_name FROM dispatchers WHERE id=NEW.dispatcher_id),'Unassigned'),
        changed_at,coalesce(current_setting('mserp.assignment_source',true),''));
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_dispatcher_history
AFTER INSERT OR UPDATE OF dispatcher_id ON drivers
FOR EACH ROW EXECUTE FUNCTION record_driver_dispatcher_assignment();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_dispatcher_assignments OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
