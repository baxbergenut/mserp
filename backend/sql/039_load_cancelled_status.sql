BEGIN;

-- Fresh schemas and upgraded databases use different names for this check.
ALTER TABLE gross_board_entries
    DROP CONSTRAINT IF EXISTS gross_board_day_status_valid,
    DROP CONSTRAINT IF EXISTS gross_board_entries_day_status_check;
ALTER TABLE gross_board_entries ADD CONSTRAINT gross_board_day_status_valid CHECK
    (day_status IN ('', 'SHOP', 'HOME', 'RESET', 'IN TRANSIT', 'REJECTED', 'LOAD CANCELLED',
     'NO LOAD', 'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'NEW DRIVER', 'DEADHEAD'));

ALTER TABLE gross_board_extra_entries DROP CONSTRAINT IF EXISTS gross_board_extra_entries_day_status_check;
ALTER TABLE gross_board_extra_entries ADD CONSTRAINT gross_board_extra_entries_day_status_check CHECK
    (day_status IN ('', 'SHOP', 'HOME', 'RESET', 'IN TRANSIT', 'REJECTED', 'LOAD CANCELLED',
     'NO LOAD', 'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'NEW DRIVER', 'DEADHEAD'));

COMMIT;
