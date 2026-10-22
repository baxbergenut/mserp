BEGIN;

-- Keep legacy field/setting names for existing clients; their values are dates.
ALTER TABLE drivers DROP CONSTRAINT IF EXISTS drivers_roster_start_week_check;

CREATE OR REPLACE FUNCTION assert_assignment_payroll_open(d uuid, t uuid, effective_week date)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE first_week date := date_trunc('week',effective_week::timestamp)::date;
BEGIN
    IF EXISTS (SELECT 1 FROM payroll_settlements
        WHERE driver_id=d AND week_start>=first_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen finalized payroll for this driver from the selected assignment date onward before changing assignments' USING ERRCODE='23514';
    END IF;
    IF EXISTS (SELECT 1 FROM investor_pay_weeks
        WHERE truck_id=t AND week_start>=first_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen finalized investor payroll for this truck from the selected assignment date onward before changing assignments' USING ERRCODE='23514';
    END IF;
END $$;

COMMIT;
