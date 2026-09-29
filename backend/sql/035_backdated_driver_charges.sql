BEGIN;
-- Backdated recurring assignments keep the type's original eligibility before
-- its first dated rule. Installment eligibility remains calendar/load weeks.
ALTER TABLE driver_charge_schedules DROP CONSTRAINT driver_charge_schedules_eligibility_check;
ALTER TABLE driver_charge_schedules ADD CONSTRAINT driver_charge_schedules_eligibility_check
    CHECK (eligibility IN ('calendar','loads') OR (kind='recurring' AND eligibility='no_loads'));
COMMIT;
