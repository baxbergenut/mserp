BEGIN;

ALTER TABLE driver_board_driver_order ADD COLUMN auto_order boolean NOT NULL DEFAULT false;

COMMIT;
