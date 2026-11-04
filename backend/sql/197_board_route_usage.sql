BEGIN;
-- Operational provider usage, unrelated to financial/payroll inputs. Reservations
-- commit before HTTP and include failed/unknown attempts; never refund on retry.
CREATE TABLE board_route_usage (
    provider text NOT NULL CHECK (provider IN ('tomtom','google')),
    month_start date NOT NULL CHECK (extract(day FROM month_start)=1),
    requests bigint NOT NULL DEFAULT 0 CHECK (requests>=0),
    paid_requests bigint NOT NULL DEFAULT 0 CHECK (paid_requests>=0 AND paid_requests<=requests),
    last_attempt_at timestamptz NOT NULL,
    PRIMARY KEY(provider,month_start)
);
INSERT INTO payroll_projection_sources(table_name,classification) VALUES ('board_route_usage','unrelated');
INSERT INTO payroll_cache_revisions(table_name,revision) VALUES ('board_route_usage',0);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE board_route_usage OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
