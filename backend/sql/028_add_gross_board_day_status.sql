ALTER TABLE gross_board_entries ADD COLUMN day_status TEXT NOT NULL DEFAULT '';
ALTER TABLE gross_board_entries ADD CONSTRAINT gross_board_day_status_valid CHECK
    (day_status IN ('', 'SHOP', 'HOME', 'RESET', 'IN TRANSIT', 'REJECTED', 'NO LOAD',
     'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'NEW DRIVER', 'DEADHEAD'));
ALTER TABLE gross_board_entries ADD CONSTRAINT gross_board_status_without_load CHECK
    (day_status = '' OR (load_number = '' AND load_record_id IS NULL
     AND original_rate IS NULL AND driver_rate IS NULL AND miles IS NULL
     AND entered_original_rate IS NULL AND entered_miles IS NULL));
