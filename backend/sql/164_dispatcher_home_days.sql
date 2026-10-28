BEGIN;
-- Keep existing reviewed input values as explicit overrides. New unsaved weeks
-- derive home days from unique HOME dates in Gross Board.
ALTER TABLE dispatcher_pay_inputs ADD COLUMN auto_home_days boolean NOT NULL DEFAULT false;
COMMIT;
