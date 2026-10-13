BEGIN;

CREATE TABLE driver_pay_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 layover_daily_amount numeric(12,2) NOT NULL DEFAULT 100 CHECK (layover_daily_amount >= 0 AND layover_daily_amount <= 999999999.99),
 version integer NOT NULL DEFAULT 1 CHECK (version > 0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by uuid REFERENCES app_users(id) ON DELETE SET NULL
);
INSERT INTO driver_pay_settings(singleton) VALUES(true);

ALTER TABLE driver_pay_weeks ADD COLUMN layover_excluded_dates text[] NOT NULL DEFAULT '{}';
ALTER TABLE driver_pay_weeks ADD CONSTRAINT driver_pay_layover_dates_limit CHECK (cardinality(layover_excluded_dates) <= 7);

DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE driver_pay_settings OWNER TO mserp_app;
 END IF;
END $$;

COMMIT;
