BEGIN;

-- Keep the dispatcher's comparison values when an imported load is matched.
ALTER TABLE gross_board_entries
    ADD COLUMN entered_original_rate NUMERIC(12,2),
    ADD COLUMN entered_miles NUMERIC(12,2) CHECK (entered_miles >= 0);
UPDATE gross_board_entries
SET entered_original_rate = original_rate, entered_miles = miles;

COMMIT;
