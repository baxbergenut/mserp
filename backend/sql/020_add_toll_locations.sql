BEGIN;

ALTER TABLE tolls
    ADD COLUMN toll_agency_state TEXT,
    ADD COLUMN toll_agency_name TEXT,
    ADD COLUMN entry_plaza_name TEXT,
    ADD COLUMN exit_plaza_name TEXT,
    ADD COLUMN location_synced_at TIMESTAMPTZ;

COMMIT;
