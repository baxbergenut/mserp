BEGIN;

-- NULL follows live weekly costs; zero and signed amounts are explicit overrides.
-- Additive: the previous binary preserves these columns when saving other edits.
ALTER TABLE driver_pay_weeks
    ADD COLUMN fuel_override NUMERIC(12,2),
    ADD COLUMN toll_override NUMERIC(12,2);

COMMIT;
