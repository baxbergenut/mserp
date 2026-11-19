BEGIN;
SELECT pg_advisory_xact_lock(736281940);
LOCK TABLE relay_fuel_sync_days, prepass_toll_sync_days, weighmytruck_sync_days IN ACCESS EXCLUSIVE MODE;
CREATE TABLE provider_sync_days (
 provider text NOT NULL,
 environment text NOT NULL,
 sync_date date NOT NULL,
 transaction_count integer NOT NULL,
 fetched_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,environment,sync_date),
 CHECK ((provider='relay' AND environment IN ('staging','production') AND transaction_count>=0)
     OR (provider='prepass' AND environment IN ('nonproduction','production') AND transaction_count>=0)
     OR (provider='weighmytruck' AND environment='default'))
);
INSERT INTO provider_sync_days(provider,environment,sync_date,transaction_count,fetched_at)
 SELECT 'relay',relay_environment,sync_date,transaction_count,fetched_at FROM relay_fuel_sync_days
 UNION ALL SELECT 'prepass',prepass_environment,sync_date,transaction_count,fetched_at FROM prepass_toll_sync_days
 UNION ALL SELECT 'weighmytruck','default',sync_date,ticket_count,synced_at FROM weighmytruck_sync_days;
CREATE INDEX provider_sync_days_scale_fetched ON provider_sync_days(fetched_at DESC) WHERE provider='weighmytruck';
DROP TABLE relay_fuel_sync_days, prepass_toll_sync_days, weighmytruck_sync_days RESTRICT;
-- Completion markers are external bookkeeping. Canonical fuel, toll and scale records
-- continue through their original payroll capture and canonical transaction.
INSERT INTO payroll_cache_revisions(table_name) VALUES('provider_sync_days');
INSERT INTO payroll_projection_sources(table_name,classification) VALUES('provider_sync_days','unrelated');
DO $$ BEGIN
 IF EXISTS (SELECT FROM pg_roles WHERE rolname='mserp_app') THEN ALTER TABLE provider_sync_days OWNER TO mserp_app; END IF;
END $$;
COMMIT;
