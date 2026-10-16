BEGIN;
-- Persist the rule on the category UUID so renaming Penalties does not remove it.
ALTER TABLE expense_settings ADD COLUMN driver_only boolean NOT NULL DEFAULT false;
UPDATE expense_settings SET driver_only=true WHERE kind='category' AND lower(btrim(name))='penalties';
ALTER TABLE expense_settings ADD CONSTRAINT expense_setting_driver_only_category CHECK(kind='category' OR NOT driver_only);
COMMIT;
