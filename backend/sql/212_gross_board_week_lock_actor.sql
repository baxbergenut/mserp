BEGIN;

-- Attribute only the current lock version, including across API rollbacks.
ALTER TABLE gross_board_week_locks
    ADD COLUMN locked_by_name text,
    ADD COLUMN locked_by_version integer;

COMMIT;
