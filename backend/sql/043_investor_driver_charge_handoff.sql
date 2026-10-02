BEGIN;

-- Called by assignment and recurring-charge writes, and once for existing
-- assignments. Recurring operating fees move; personal installments do not.
CREATE FUNCTION handoff_investor_driver_charges(p_driver UUID, p_actor UUID DEFAULT NULL)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    assignment RECORD;
    schedule RECORD;
    phase RECORD;
    effective DATE;
    cutoff DATE;
    amount_at_start NUMERIC;
    paused_at_start BOOLEAN;
    detail JSONB;
BEGIN
    PERFORM pg_advisory_xact_lock_shared(736281940);
    PERFORM 1 FROM drivers WHERE id=p_driver FOR UPDATE;
    SELECT a.truck_id, date_trunc('week',greatest(a.assigned_at,CASE WHEN h.start_known THEN h.assigned_at END) AT TIME ZONE 'America/New_York')::date AS week
      INTO assignment
      FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
      JOIN investors i ON i.id=t.owner_id
      LEFT JOIN truck_ownership_history h ON h.truck_id=t.id AND h.owner_id=i.id AND h.unassigned_at IS NULL
      WHERE a.driver_id=p_driver AND a.unassigned_at IS NULL AND NOT i.is_company
        AND (i.driver_id IS NULL OR (i.driver_id<>p_driver AND
          (SELECT count(*) FROM trucks owned WHERE owned.owner_id=i.id)>=2));
    IF NOT FOUND THEN RETURN; END IF;
    -- Serialize fee seeding when replacement drivers share a truck/week.
    PERFORM 1 FROM trucks WHERE id=assignment.truck_id FOR UPDATE;
    FOR schedule IN SELECT * FROM driver_charge_schedules
      WHERE driver_id=p_driver AND kind='recurring' AND direction='charge'
      ORDER BY start_week,id FOR UPDATE
    LOOP
      effective := greatest(assignment.week,schedule.start_week);
      IF schedule.end_week IS NOT NULL AND schedule.end_week<effective THEN CONTINUE; END IF;
      SELECT amount,paused INTO amount_at_start,paused_at_start FROM driver_charge_phases
        WHERE schedule_id=schedule.id AND week_start<=effective ORDER BY week_start DESC LIMIT 1;
      IF amount_at_start IS NULL THEN CONTINUE; END IF;
      IF paused_at_start AND NOT EXISTS(SELECT 1 FROM driver_charge_phases
           WHERE schedule_id=schedule.id AND week_start>effective AND NOT paused)
         AND NOT EXISTS(SELECT 1 FROM driver_charge_occurrences WHERE schedule_id=schedule.id AND week_start>=effective)
      THEN CONTINUE; END IF;
      IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=p_driver AND week_start>=effective AND finalized)
        OR EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=assignment.truck_id AND week_start>=effective AND finalized)
        OR EXISTS(SELECT 1 FROM driver_charge_occurrences WHERE schedule_id=schedule.id AND week_start>=effective AND confirmed_at IS NOT NULL)
      THEN RAISE EXCEPTION 'Reopen affected settlements before moving driver fees to an investor truck' USING ERRCODE='23514'; END IF;
      SELECT jsonb_build_object('truckId',assignment.truck_id,'weekStart',effective,
        'phases',coalesce((SELECT jsonb_agg(to_jsonb(p)) FROM driver_charge_phases p WHERE schedule_id=schedule.id),'[]'::jsonb),
        'occurrences',coalesce((SELECT jsonb_agg(to_jsonb(o)) FROM driver_charge_occurrences o WHERE schedule_id=schedule.id AND week_start>=effective),'[]'::jsonb)) INTO detail;
      -- Existing truck selections win, including explicit pauses. Only fill
      -- the gap before the first configured phase; never stack the same type.
      IF NOT EXISTS(SELECT 1 FROM truck_charge_phases WHERE truck_id=assignment.truck_id AND type_id=schedule.type_id AND week_start<=effective) THEN
        SELECT min(week_start) INTO cutoff FROM truck_charge_phases WHERE truck_id=assignment.truck_id AND type_id=schedule.type_id;
        INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by)
          VALUES(assignment.truck_id,schedule.type_id,effective,amount_at_start,NOT paused_at_start,p_actor);
        FOR phase IN SELECT * FROM driver_charge_phases WHERE schedule_id=schedule.id AND week_start>effective
            AND (cutoff IS NULL OR week_start<cutoff) AND (schedule.end_week IS NULL OR week_start<=schedule.end_week) ORDER BY week_start
        LOOP
          INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by)
            VALUES(assignment.truck_id,schedule.type_id,phase.week_start,phase.amount,NOT phase.paused,p_actor);
        END LOOP;
        IF schedule.end_week IS NOT NULL AND (cutoff IS NULL OR schedule.end_week+7<cutoff) THEN
          INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included,updated_by)
            VALUES(assignment.truck_id,schedule.type_id,schedule.end_week+7,amount_at_start,false,p_actor) ON CONFLICT DO NOTHING;
        END IF;
      END IF;
      DELETE FROM driver_charge_occurrences WHERE schedule_id=schedule.id AND week_start>=effective;
      DELETE FROM driver_charge_phases WHERE schedule_id=schedule.id AND week_start>=effective;
      INSERT INTO driver_charge_phases(schedule_id,week_start,amount,paused) VALUES(schedule.id,effective,amount_at_start,true);
      UPDATE driver_charge_schedules SET version=version+1 WHERE id=schedule.id;
      INSERT INTO driver_charge_events(schedule_id,type_id,actor_id,action,details)
        VALUES(schedule.id,schedule.type_id,p_actor,'investor_truck_handoff',detail);
      INSERT INTO truck_settlement_events(truck_id,week_start,actor_id,action,details)
        VALUES(assignment.truck_id,effective,p_actor,'driver_fee_handoff',detail);
    END LOOP;
END $$;

DO $$ DECLARE driver UUID; BEGIN
  FOR driver IN SELECT driver_id FROM truck_driver_assignments WHERE unassigned_at IS NULL ORDER BY driver_id
  LOOP PERFORM handoff_investor_driver_charges(driver); END LOOP;
END $$;

COMMIT;
