BEGIN;

ALTER TABLE five_eld_locations ADD COLUMN heading DOUBLE PRECISION
    CHECK (heading >= 0 AND heading < 360);

COMMIT;
