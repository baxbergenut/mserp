BEGIN;
-- Completion remains authoritative for older clients using completed_at.
ALTER TABLE custom_tasks ADD COLUMN in_process BOOLEAN NOT NULL DEFAULT false;
COMMIT;
