BEGIN;
-- Compatibility bridge: deploy company-independent readers/writers before 088
-- removes the column. Older API binaries remain usable during this activation.
ALTER TABLE expenses ALTER COLUMN company SET DEFAULT '';
COMMIT;
