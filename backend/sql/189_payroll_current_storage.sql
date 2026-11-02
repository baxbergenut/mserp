-- Quiesced cutover: exactly one current statement, no statement history.
BEGIN;
SELECT pg_advisory_xact_lock(736281941);
ALTER TABLE payroll_statement_versions RENAME TO payroll_statement_versions_legacy;
ALTER TABLE payroll_projection_generations RENAME TO payroll_projection_generations_legacy;
ALTER TABLE payroll_statement_state DROP CONSTRAINT payroll_statement_state_version_id_kind_statement_id_week__fkey;
ALTER TABLE payroll_command_versions DROP CONSTRAINT payroll_command_versions_version_id_fkey;
ALTER TABLE payroll_projection_weeks DROP CONSTRAINT payroll_projection_weeks_generation_id_week_start_fkey;

CREATE TABLE payroll_projection_generations
 (LIKE payroll_projection_generations_legacy INCLUDING DEFAULTS INCLUDING CONSTRAINTS,
 PRIMARY KEY(id), UNIQUE(id,week_start));
CREATE TABLE payroll_statement_current
 (LIKE payroll_statement_versions_legacy INCLUDING DEFAULTS INCLUDING CONSTRAINTS,
 PRIMARY KEY(id), UNIQUE(kind,statement_id,week_start), UNIQUE(id,kind,statement_id,week_start),
 FOREIGN KEY(generation_id,week_start) REFERENCES payroll_projection_generations(id,week_start));
CREATE INDEX payroll_current_week ON payroll_statement_current(kind,week_start,statement_id);
CREATE INDEX payroll_current_generation ON payroll_statement_current(generation_id);
CREATE INDEX payroll_current_dependency ON payroll_statement_current((resource_dependencies->>'$shared'));
-- References retain an audit token and hashes, never a prior statement body.
CREATE TABLE payroll_statement_references (
 id uuid PRIMARY KEY,kind text NOT NULL,statement_id uuid NOT NULL,week_start date NOT NULL,
 detail_hash text NOT NULL,cost_hash text NOT NULL
);
CREATE TABLE payroll_storage_cutovers (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 previous_copies bigint NOT NULL, current_statements bigint NOT NULL, retained_references bigint NOT NULL,
 current_digest text NOT NULL, receipt_digest text NOT NULL
);
CREATE FUNCTION payroll_intern_dependencies(value jsonb) RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE expanded jsonb; fingerprint_value text;
BEGIN
 expanded:=payroll_expand_resource_dependencies(value);
 IF expanded IS NULL THEN RAISE EXCEPTION 'Missing payroll dependencies'; END IF;
 IF octet_length(expanded::text)<2048 THEN RETURN expanded; END IF;
 fingerprint_value:=encode(sha256(convert_to(expanded::text,'UTF8')),'hex');
 INSERT INTO payroll_resource_dependency_sets(fingerprint,dependencies) VALUES(fingerprint_value,expanded) ON CONFLICT DO NOTHING;
 IF NOT EXISTS(SELECT 1 FROM payroll_resource_dependency_sets WHERE fingerprint=fingerprint_value AND dependencies=expanded) THEN
  RAISE EXCEPTION 'Payroll dependency hash mismatch';
 END IF;
 RETURN jsonb_build_object('$shared',fingerprint_value);
END $$;

CREATE TEMP TABLE payroll_storage_roots ON COMMIT DROP AS
 SELECT version_id AS id FROM payroll_command_versions
 UNION SELECT v.id FROM payroll_statement_versions_legacy v JOIN payroll_command_receipts r ON r.expected_revision=v.id::text
 UNION SELECT v.id FROM payroll_command_receipts r CROSS JOIN LATERAL jsonb_path_query(r.result,'$.** ? (@.type() == "string")') j(value)
 JOIN payroll_statement_versions_legacy v ON v.id::text=j.value#>>'{}';
CREATE UNIQUE INDEX ON payroll_storage_roots(id);
CREATE TEMP TABLE payroll_storage_proof ON COMMIT DROP AS
 SELECT (SELECT count(*) FROM payroll_statement_versions_legacy) copies,
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(v)::text),'' ORDER BY v.id),'')) FROM payroll_statement_state s JOIN payroll_statement_versions_legacy v ON v.id=s.version_id) current_digest,
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(r)::text),'' ORDER BY actor_id,request_id),'')) FROM payroll_command_receipts r) receipt_digest;

INSERT INTO payroll_projection_generations
 SELECT g.* FROM payroll_projection_generations_legacy g WHERE EXISTS(SELECT 1 FROM payroll_projection_weeks w WHERE w.generation_id=g.id)
 OR EXISTS(SELECT 1 FROM payroll_statement_versions_legacy v JOIN payroll_statement_state s ON s.version_id=v.id WHERE v.generation_id=g.id);
INSERT INTO payroll_statement_current SELECT v.* FROM payroll_statement_versions_legacy v JOIN payroll_statement_state s ON s.version_id=v.id;
INSERT INTO payroll_statement_references
 SELECT v.id,v.kind,v.statement_id,v.week_start,md5(v.detail::text),md5(v.cost_evidence::text)
 FROM payroll_statement_versions_legacy v JOIN payroll_storage_roots r ON r.id=v.id;
DO $$ BEGIN
 IF (SELECT current_digest FROM payroll_storage_proof) IS DISTINCT FROM
 (SELECT md5(coalesce(string_agg(md5(to_jsonb(v)::text),'' ORDER BY v.id),'')) FROM payroll_statement_current v) THEN
  RAISE EXCEPTION 'Payroll current statement cutover mismatch';
 END IF;
 IF EXISTS(SELECT 1 FROM payroll_storage_roots r LEFT JOIN payroll_statement_references a ON a.id=r.id WHERE a.id IS NULL)
 OR EXISTS(SELECT 1 FROM payroll_statement_references a JOIN payroll_statement_versions_legacy v USING(id)
 WHERE (a.kind,a.statement_id,a.week_start,a.detail_hash,a.cost_hash) IS DISTINCT FROM (v.kind,v.statement_id,v.week_start,md5(v.detail::text),md5(v.cost_evidence::text))) THEN
  RAISE EXCEPTION 'Payroll audit reference cutover mismatch';
 END IF;
END $$;
UPDATE payroll_statement_current SET resource_dependencies=payroll_intern_dependencies(resource_dependencies);

CREATE VIEW payroll_statement_versions AS SELECT * FROM payroll_statement_current;
ALTER VIEW payroll_statement_versions ALTER COLUMN id SET DEFAULT gen_random_uuid();
CREATE FUNCTION payroll_retain_statement_reference(version uuid) RETURNS void LANGUAGE sql AS $$
 INSERT INTO payroll_statement_references
 SELECT id,kind,statement_id,week_start,md5(detail::text),md5(cost_evidence::text) FROM payroll_statement_current WHERE id=version ON CONFLICT(id) DO NOTHING
$$;
CREATE FUNCTION payroll_replace_statement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous payroll_statement_current%ROWTYPE;
BEGIN
 SELECT * INTO previous FROM payroll_statement_current WHERE kind=NEW.kind AND statement_id=NEW.statement_id AND week_start=NEW.week_start FOR UPDATE;
 IF FOUND AND (to_jsonb(previous)-'id'-'generation_id')=(to_jsonb(NEW)-'id'-'generation_id') THEN
  NEW.id:=previous.id;
  RETURN NEW;
 END IF;
 INSERT INTO payroll_statement_current SELECT NEW.*
 ON CONFLICT(kind,statement_id,week_start) DO UPDATE SET
 id=excluded.id,generation_id=excluded.generation_id,detail=excluded.detail,summary=excluded.summary,
 cost_evidence=excluded.cost_evidence,resource_dependencies=excluded.resource_dependencies,
 fee_cents=excluded.fee_cents,payable_cents=excluded.payable_cents,finalized=excluded.finalized,
 edit_version=excluded.edit_version,settlement_version=excluded.settlement_version;
 RETURN NEW;
END $$;
CREATE TRIGGER payroll_replace_statement INSTEAD OF INSERT ON payroll_statement_versions
 FOR EACH ROW EXECUTE FUNCTION payroll_replace_statement();
CREATE FUNCTION guard_payroll_storage_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Payroll audit records are immutable'; END $$;
CREATE TRIGGER payroll_statement_write_path INSTEAD OF UPDATE OR DELETE ON payroll_statement_versions
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_storage_audit();
ALTER TABLE payroll_statement_state ADD FOREIGN KEY(version_id,kind,statement_id,week_start)
 REFERENCES payroll_statement_current(id,kind,statement_id,week_start) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE payroll_command_versions ADD FOREIGN KEY(version_id) REFERENCES payroll_statement_references(id);
CREATE FUNCTION payroll_record_command_output() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM payroll_retain_statement_reference(NEW.version_id); RETURN NEW; END $$;
CREATE TRIGGER payroll_record_command_output BEFORE INSERT ON payroll_command_versions
 FOR EACH ROW EXECUTE FUNCTION payroll_record_command_output();
ALTER TABLE payroll_projection_weeks ADD FOREIGN KEY(generation_id,week_start) REFERENCES payroll_projection_generations(id,week_start);
CREATE FUNCTION payroll_remove_ineligible_current() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM payroll_statement_current c WHERE c.kind=OLD.kind AND c.statement_id=OLD.statement_id AND c.week_start=OLD.week_start
 AND NOT EXISTS(SELECT 1 FROM payroll_statement_state s WHERE s.kind=c.kind AND s.statement_id=c.statement_id AND s.week_start=c.week_start);
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER payroll_current_membership AFTER DELETE OR UPDATE ON payroll_statement_state
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION payroll_remove_ineligible_current();
CREATE TRIGGER payroll_reference_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payroll_statement_references
 FOR EACH STATEMENT EXECUTE FUNCTION guard_payroll_storage_audit();
CREATE TRIGGER payroll_storage_receipt_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON payroll_storage_cutovers
 FOR EACH STATEMENT EXECUTE FUNCTION guard_payroll_storage_audit();
CREATE OR REPLACE FUNCTION guard_payroll_projection_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND TG_TABLE_NAME IN ('payroll_projection_generations','payroll_load_facts','payroll_routing_facts')
 AND coalesce(current_setting('mserp.payroll_storage_maintenance',true),'')='on' THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'Payroll projection versions and command receipts are immutable' USING ERRCODE='23514';
END $$;
CREATE TRIGGER payroll_projection_generation_immutable BEFORE UPDATE OR DELETE ON payroll_projection_generations
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_projection_history();
CREATE OR REPLACE FUNCTION payroll_dependency_set_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND coalesce(current_setting('mserp.payroll_storage_maintenance',true),'')='on'
 AND NOT EXISTS(SELECT 1 FROM payroll_statement_current WHERE resource_dependencies->>'$shared'=OLD.fingerprint) THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'Payroll dependency snapshots are immutable';
END $$;
INSERT INTO payroll_storage_cutovers(previous_copies,current_statements,retained_references,current_digest,receipt_digest)
 SELECT copies,(SELECT count(*) FROM payroll_statement_current),(SELECT count(*) FROM payroll_statement_references),current_digest,receipt_digest FROM payroll_storage_proof;
-- Canonical financial tables, frozen settlement/event reports and exact request
-- receipts remain untouched. DROP releases old heap/TOAST/index space at COMMIT.
DROP TABLE payroll_statement_versions_legacy;
DROP TABLE payroll_projection_generations_legacy;
TRUNCATE payroll_routing_facts,payroll_load_facts;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['payroll_statement_current','payroll_statement_references','payroll_storage_cutovers'] LOOP
  INSERT INTO payroll_projection_sources VALUES(t,'derived');
  INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE payroll_projection_generations OWNER TO mserp_app;
  ALTER TABLE payroll_statement_current OWNER TO mserp_app;
  ALTER TABLE payroll_statement_references OWNER TO mserp_app;
  ALTER TABLE payroll_storage_cutovers OWNER TO mserp_app;
  ALTER VIEW payroll_statement_versions OWNER TO mserp_app;
 END IF;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='fleet_review_portal') THEN
  GRANT SELECT ON payroll_statement_versions,payroll_statement_current,payroll_statement_references,payroll_storage_cutovers,payroll_projection_generations TO fleet_review_portal;
 END IF;
END $$;
COMMIT;
