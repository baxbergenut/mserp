BEGIN;

-- One transactional revision per table. Changes and invalidation commit together.
-- Authentication/session housekeeping is deliberately outside financial inputs.
CREATE TABLE payroll_cache_revisions (
    table_name TEXT PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 0
);
CREATE FUNCTION invalidate_payroll_cache() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'TRUNCATE' THEN
        IF NOT EXISTS (SELECT 1 FROM changed_rows LIMIT 1) THEN RETURN NULL; END IF;
    END IF;
    UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name=TG_TABLE_NAME;
    RETURN NULL;
END $$;
DO $$
DECLARE item RECORD;
BEGIN
    -- Cover every existing application table, not a fragile subset of payroll joins.
    FOR item IN SELECT tablename FROM pg_tables WHERE schemaname=current_schema()
        AND tablename NOT IN ('payroll_cache_revisions','auth_sessions','schema_migrations')
        ORDER BY tablename
    LOOP
        INSERT INTO payroll_cache_revisions(table_name) VALUES(item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
        EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',item.tablename);
    END LOOP;
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE payroll_cache_revisions OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
