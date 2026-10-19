BEGIN;

-- Driver Board > TODAY!J4 choices. Keep legacy values valid so existing rows
-- and audit/undo snapshots are preserved without guessing a replacement.
ALTER TABLE driver_board DROP CONSTRAINT driver_board_status_check;
ALTER TABLE driver_board ADD CONSTRAINT driver_board_status_check CHECK (status IN (
    '', 'READY', 'REST', 'COVERED', 'DISPATCHED', 'ENROUTE', 'RESERVED',
    'VACATION', 'DEADHEAD', 'HOME', 'SHOP', 'STOP', 'In the YARD',
    'Hold for Load', 'Left', 'Family issue', 'Issue with PU/Del', 'At PU',
    'Health issue', 'At DEL', 'HOME (YARD)', 'NEW DRIVER', '34 RESET', 'ACCIDENT',
    'RESET', 'NO LOAD', 'STUCK', 'LATE DEL', 'TRUCK ISSUE', 'LEFT', 'LOAD CANCELLED', 'REJECTED'
));

COMMIT;
