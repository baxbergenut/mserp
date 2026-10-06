BEGIN;
-- Audit snapshots survive deletion of unused catalog entries.
ALTER TABLE driver_charge_events DROP CONSTRAINT driver_charge_events_type_id_fkey;
ALTER TABLE driver_charge_events ADD CONSTRAINT driver_charge_events_type_id_fkey
    FOREIGN KEY(type_id) REFERENCES driver_charge_types(id) ON DELETE SET NULL;
COMMIT;
