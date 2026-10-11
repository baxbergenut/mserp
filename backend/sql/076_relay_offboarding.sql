BEGIN;
ALTER TABLE relay_driver_operations ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','termination'));
ALTER TABLE relay_driver_operations ADD COLUMN termination_date DATE;
COMMIT;
