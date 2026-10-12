BEGIN;

-- Driver classification is a read of assignment and ownership, never user input.
-- A date selects historical relationships; NULL selects the current assignment.
CREATE OR REPLACE FUNCTION driver_employment_type(p_driver uuid, p_day date DEFAULT NULL)
RETURNS text LANGUAGE sql STABLE AS $$
 SELECT coalesce((
   SELECT CASE WHEN i.is_company THEN 'company' WHEN i.driver_id=p_driver THEN 'owner_operator' ELSE 'investor_driver' END
   FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
   LEFT JOIN LATERAL (
     SELECT h.owner_id FROM truck_ownership_history h WHERE h.truck_id=t.id
       AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<=p_day)
       AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>p_day)
     ORDER BY h.assigned_at DESC LIMIT 1
   ) ownership ON p_day IS NOT NULL
   JOIN investors i ON i.id=CASE WHEN p_day IS NULL THEN t.owner_id ELSE ownership.owner_id END
   WHERE a.driver_id=payroll_driver_at(p_driver,coalesce(p_day,(now() AT TIME ZONE 'America/New_York')::date))
     AND CASE WHEN p_day IS NULL THEN a.unassigned_at IS NULL ELSE
       (a.assigned_at AT TIME ZONE 'America/New_York')::date<=p_day AND
       (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>p_day) END
   ORDER BY a.assigned_at DESC LIMIT 1
 ),'unassigned')
$$;

-- Explicitly authorized reset of driver recurring assignments. Installment
-- principal and frozen payroll snapshots are separate and remain intact.
ALTER TABLE driver_charge_events DROP CONSTRAINT driver_charge_events_schedule_id_fkey;
ALTER TABLE driver_charge_events ADD CONSTRAINT driver_charge_events_schedule_id_fkey
 FOREIGN KEY(schedule_id) REFERENCES driver_charge_schedules(id) ON DELETE SET NULL;
INSERT INTO driver_charge_events(schedule_id,type_id,action,details)
 SELECT id,type_id,'retired_driver_recurring',to_jsonb(s) FROM driver_charge_schedules s WHERE kind='recurring';
-- Keep frozen report JSON intact, but remove retired collection handles so a
-- later explicit reopen does not try to audit a deleted schedule.
UPDATE payroll_settlements w SET confirmed_schedules=ARRAY(
 SELECT kept.schedule_id FROM unnest(w.confirmed_schedules) AS kept(schedule_id)
 WHERE NOT EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.id=kept.schedule_id AND s.kind='recurring'))
 WHERE confirmed_schedules && ARRAY(SELECT id FROM driver_charge_schedules WHERE kind='recurring');
-- This one-time authorized reset includes already-confirmed recurring rows.
-- Transactional DDL holds the table lock and restores the guard before commit.
ALTER TABLE driver_charge_occurrences DISABLE TRIGGER payroll_charge_guard;
DELETE FROM driver_charge_occurrences WHERE schedule_id IN (SELECT id FROM driver_charge_schedules WHERE kind='recurring');
ALTER TABLE driver_charge_occurrences ENABLE TRIGGER payroll_charge_guard;
DELETE FROM driver_charge_phases WHERE schedule_id IN (SELECT id FROM driver_charge_schedules WHERE kind='recurring');
DELETE FROM driver_charge_schedules WHERE kind='recurring';
ALTER TABLE driver_charge_schedules DROP CONSTRAINT IF EXISTS driver_charges_installments_only;
ALTER TABLE driver_charge_schedules ADD CONSTRAINT driver_charges_installments_only CHECK (kind='installment');

-- Remove the specifically requested catalog entry and all its truck selections.
INSERT INTO driver_charge_events(type_id,action,details)
 SELECT id,'type_deleted',to_jsonb(t) FROM driver_charge_types t WHERE lower(btrim(name))='charge';
DELETE FROM truck_charge_phases WHERE type_id IN (SELECT id FROM driver_charge_types WHERE lower(btrim(name))='charge');
DELETE FROM driver_charge_type_rules WHERE type_id IN (SELECT id FROM driver_charge_types WHERE lower(btrim(name))='charge');
DELETE FROM driver_charge_types WHERE lower(btrim(name))='charge';

-- Older binaries may still call this function during assignment changes.
CREATE OR REPLACE FUNCTION handoff_investor_driver_charges(p_driver uuid, p_actor uuid DEFAULT NULL)
RETURNS void LANGUAGE plpgsql AS $$ BEGIN RETURN; END $$;

COMMIT;
