BEGIN;
-- Deploy the company-independent API from migration 087 before applying this.
-- The immediately preceding release remains compatible with this schema.
-- Dropping the column also removes its company/date index and historical values.
ALTER TABLE expenses DROP COLUMN company;
COMMIT;
