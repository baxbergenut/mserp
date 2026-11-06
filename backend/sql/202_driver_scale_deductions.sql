ALTER TABLE driver_pay_weeks ADD COLUMN scale_deductions jsonb NOT NULL DEFAULT '{}'
 CHECK (jsonb_typeof(scale_deductions) = 'object');
