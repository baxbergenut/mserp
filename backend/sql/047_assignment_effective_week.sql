BEGIN;

-- Keep the old trigger contract for imports and older binaries. Fleet forms set
-- this transaction-local week explicitly (New York midnight, including DST).
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
        IF EXISTS (SELECT 1 FROM driver_dispatcher_assignments
            WHERE driver_id=NEW.id AND start_known
            AND assigned_at >= ((effective_week+7)::timestamp AT TIME ZONE 'America/New_York')) THEN
            RAISE EXCEPTION 'The selected week precedes a later dispatcher assignment; choose that week or a later one' USING ERRCODE='23514';
        END IF;
        -- Preserve multiple changes in one week as zero-length history records,
        -- moving both ends together so the previous week remains untouched.
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
