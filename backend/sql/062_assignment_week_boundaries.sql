BEGIN;

-- A reactivated driver's current roster starts independently of old assignments.
ALTER TABLE drivers ADD COLUMN roster_start_week date
    CHECK (extract(isodow FROM roster_start_week)=1);
-- Only backfill known starts; migration observation dates are not start dates.
UPDATE drivers d SET roster_start_week=date_trunc('week',s.started_at AT TIME ZONE 'America/New_York')::date
FROM (
    SELECT driver_id,min(assigned_at) started_at FROM (
        SELECT driver_id,assigned_at FROM driver_dispatcher_assignments
        UNION ALL SELECT driver_id,assigned_at FROM truck_driver_assignments
    ) h GROUP BY driver_id
) s WHERE s.driver_id=d.id AND NOT EXISTS (
    SELECT 1 FROM driver_dispatcher_assignments h WHERE h.driver_id=d.id AND NOT h.start_known
);

-- Assignment changes may use any Monday, but cannot rewrite settled periods.
CREATE OR REPLACE FUNCTION assert_assignment_payroll_open(d uuid, t uuid, effective_week date)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM payroll_settlements
        WHERE driver_id=d AND week_start>=effective_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen finalized payroll for this driver from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
    END IF;
    IF EXISTS (SELECT 1 FROM investor_pay_weeks
        WHERE truck_id=t AND week_start>=effective_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen finalized investor payroll for this truck from the selected assignment week onward before changing assignments' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION record_driver_roster_start() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE effective_week date := nullif(current_setting('mserp.assignment_week',true),'')::date;
BEGIN
    IF effective_week IS NULL THEN RETURN NEW; END IF;
    IF TG_OP='INSERT' THEN
        NEW.roster_start_week := effective_week;
    ELSIF NEW.active AND NOT OLD.active THEN
        PERFORM assert_assignment_payroll_open(NEW.id,NULL,effective_week);
        NEW.roster_start_week := effective_week;
    ELSIF NEW.dispatcher_id IS DISTINCT FROM OLD.dispatcher_id AND NEW.roster_start_week>effective_week THEN
        -- An explicit earlier assignment also brings the current roster forward.
        NEW.roster_start_week := effective_week;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER drivers_roster_start BEFORE INSERT OR UPDATE OF active,dispatcher_id ON drivers
FOR EACH ROW EXECUTE FUNCTION record_driver_roster_start();

-- Imports without an explicit accounting week retain their observed timestamps.
CREATE OR REPLACE FUNCTION record_driver_dispatcher_assignment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
DECLARE effective_week date := nullif(current_setting('mserp.assignment_week',true),'')::date;
BEGIN
    IF TG_OP='UPDATE' AND NEW.dispatcher_id IS NOT DISTINCT FROM OLD.dispatcher_id THEN
        RETURN NEW;
    END IF;
    IF effective_week IS NOT NULL THEN
        changed_at := effective_week::timestamp AT TIME ZONE 'America/New_York';
        PERFORM assert_assignment_payroll_open(NEW.id,NULL,effective_week);
        -- Superseded changes remain as zero-length history records. Earlier
        -- periods keep their assignments up to the newly selected boundary.
        UPDATE driver_dispatcher_assignments SET
            assigned_at=least(assigned_at,changed_at),
            unassigned_at=CASE WHEN unassigned_at IS NULL THEN NULL ELSE least(unassigned_at,changed_at) END
        WHERE driver_id=NEW.id AND (assigned_at>=changed_at OR unassigned_at>=changed_at);
    END IF;
    UPDATE driver_dispatcher_assignments SET unassigned_at=changed_at
    WHERE driver_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,source)
    VALUES(NEW.id,NEW.dispatcher_id,
        coalesce((SELECT full_name FROM dispatchers WHERE id=NEW.dispatcher_id),'Unassigned'),
        changed_at,coalesce(current_setting('mserp.assignment_source',true),''));
    RETURN NEW;
END $$;

COMMIT;
