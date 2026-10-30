BEGIN;

-- Installed inactive. An explicit, immutable operator receipt establishes the
-- first live Fuel/Toll week without changing expenses or historical statements.
CREATE TABLE payroll_cost_start (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 2000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION protect_payroll_cost_start() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'The Fuel/Toll starting boundary is an immutable receipt' USING ERRCODE='23514'; END $$;
CREATE TRIGGER payroll_cost_start_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payroll_cost_start
 FOR EACH STATEMENT EXECUTE FUNCTION protect_payroll_cost_start();
CREATE FUNCTION payroll_cost_in_scope(source_week date, statement_week date) RETURNS boolean
 LANGUAGE sql STABLE AS $$
 SELECT NOT EXISTS(SELECT 1 FROM payroll_cost_start b
 WHERE source_week<b.week_start AND (statement_week IS NULL OR statement_week>=b.week_start))
$$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE payroll_cost_start OWNER TO mserp_app;
 END IF;
END $$;
INSERT INTO payroll_cache_revisions(table_name) VALUES('payroll_cost_start');
CREATE TRIGGER payroll_cache_insert AFTER INSERT ON payroll_cost_start REFERENCING NEW TABLE AS changed_rows
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON payroll_cost_start
 FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
COMMIT;
