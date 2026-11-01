-- Share repeated revision maps without changing their decoded dependencies.
-- Existing immutable statement versions remain untouched and readable.
BEGIN;
ALTER TABLE payroll_projection_control ADD COLUMN first_week date
 CHECK (extract(isodow FROM first_week)=1 AND first_week>='2000-01-03');
CREATE FUNCTION guard_payroll_available_week() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.week_start < (SELECT first_week FROM payroll_projection_control WHERE singleton) THEN
   RAISE EXCEPTION 'This payroll week has been retired' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payroll_available_week BEFORE INSERT OR UPDATE ON driver_pay_weeks FOR EACH ROW EXECUTE FUNCTION guard_payroll_available_week();
CREATE TRIGGER payroll_available_week BEFORE INSERT OR UPDATE ON investor_pay_weeks FOR EACH ROW EXECUTE FUNCTION guard_payroll_available_week();
CREATE TRIGGER payroll_available_week BEFORE INSERT OR UPDATE ON payroll_settlements FOR EACH ROW EXECUTE FUNCTION guard_payroll_available_week();

CREATE TABLE payroll_retirement_receipts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), first_week date NOT NULL,
 driver_weeks integer NOT NULL, investor_weeks integer NOT NULL,
 pinned_costs integer NOT NULL, verified_weeks integer NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO payroll_projection_sources(table_name,classification) VALUES('payroll_retirement_receipts','derived');
INSERT INTO payroll_cache_revisions(table_name) VALUES('payroll_retirement_receipts');

CREATE TABLE payroll_resource_dependency_sets (
    fingerprint text PRIMARY KEY CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    dependencies jsonb NOT NULL CHECK (jsonb_typeof(dependencies)='object' AND NOT dependencies ? '$shared'),
    CHECK (fingerprint=encode(sha256(convert_to(dependencies::text,'UTF8')),'hex'))
);
INSERT INTO payroll_projection_sources(table_name,classification)
VALUES('payroll_resource_dependency_sets','derived');
INSERT INTO payroll_cache_revisions(table_name) VALUES('payroll_resource_dependency_sets');
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
   ALTER TABLE payroll_resource_dependency_sets OWNER TO mserp_app;
   ALTER TABLE payroll_retirement_receipts OWNER TO mserp_app;
 END IF;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='fleet_review_portal') THEN
   GRANT SELECT ON payroll_resource_dependency_sets TO fleet_review_portal;
 END IF;
END $$;

CREATE FUNCTION payroll_expand_resource_dependencies(value jsonb) RETURNS jsonb
LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN value ? '$shared' THEN
   (SELECT dependencies || (value - '$shared') FROM payroll_resource_dependency_sets
    WHERE fingerprint=value->>'$shared')
 ELSE value END
$$;

CREATE FUNCTION payroll_pack_resource_snapshot(keys text[], statement_key text) RETURNS jsonb
LANGUAGE plpgsql AS $$
DECLARE snapshot jsonb; shared jsonb; fingerprint_value text; retained jsonb;
BEGIN
 snapshot := payroll_resource_snapshot(keys);
 -- Small maps do not benefit from an extra lookup. The statement revision stays
 -- local so otherwise identical component maps can be shared across statements.
 IF octet_length(snapshot::text)<2048 THEN RETURN snapshot; END IF;
 shared := snapshot - statement_key;
 fingerprint_value := encode(sha256(convert_to(shared::text,'UTF8')),'hex');
 INSERT INTO payroll_resource_dependency_sets(fingerprint,dependencies)
 VALUES(fingerprint_value,shared) ON CONFLICT DO NOTHING;
 SELECT dependencies INTO retained FROM payroll_resource_dependency_sets WHERE fingerprint=fingerprint_value;
 IF retained IS DISTINCT FROM shared THEN
   RAISE EXCEPTION 'Payroll dependency snapshot is unavailable' USING ERRCODE='40001';
 END IF;
 RETURN jsonb_build_object('$shared',fingerprint_value) ||
   CASE WHEN snapshot ? statement_key THEN jsonb_build_object(statement_key,snapshot->statement_key) ELSE '{}'::jsonb END;
END
$$;

CREATE OR REPLACE FUNCTION payroll_resources_current(dependencies jsonb) RETURNS boolean
LANGUAGE sql STABLE AS $$
 WITH expanded AS (SELECT payroll_expand_resource_dependencies(dependencies) value)
 SELECT value IS NOT NULL
 AND ((SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton) OR value<>'{}')
 AND NOT EXISTS(SELECT 1 FROM jsonb_each_text(value) d(key,value)
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=d.key WHERE coalesce(r.revision,0)<>d.value::bigint)
 FROM expanded
$$;

CREATE FUNCTION payroll_dependency_set_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Payroll dependency snapshots are immutable'; END
$$;
CREATE TRIGGER payroll_dependency_set_immutable BEFORE UPDATE OR DELETE ON payroll_resource_dependency_sets
FOR EACH ROW EXECUTE FUNCTION payroll_dependency_set_immutable();
CREATE TRIGGER payroll_dependency_set_no_truncate BEFORE TRUNCATE ON payroll_resource_dependency_sets
FOR EACH STATEMENT EXECUTE FUNCTION payroll_dependency_set_immutable();

COMMIT;
