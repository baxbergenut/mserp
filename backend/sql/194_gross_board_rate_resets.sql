-- Rate balance checkpoints are Gross Board accounting metadata. Keep storage
-- outside the payroll source schema so existing payroll releases can roll back.
BEGIN;
CREATE SCHEMA IF NOT EXISTS gross_board_accounting;
CREATE TABLE IF NOT EXISTS gross_board_accounting.rate_resets (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 driver_id uuid NOT NULL,
 week_start date NOT NULL CHECK (extract(isodow FROM week_start)=1),
 target numeric(12,2) NOT NULL,
 week_change numeric NOT NULL,
 actor_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS rate_resets_driver_week ON gross_board_accounting.rate_resets(driver_id,week_start DESC,id DESC);
CREATE VIEW gross_board_rate_resets AS SELECT * FROM gross_board_accounting.rate_resets;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  GRANT SELECT, INSERT ON gross_board_rate_resets TO mserp_app;
 END IF;
END $$;
COMMIT;
