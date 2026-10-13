BEGIN;
CREATE TABLE updater_extension_access (
    updater_id UUID PRIMARY KEY REFERENCES updaters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT false,
    account_label TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    activation_hash TEXT UNIQUE,
    activation_expires_at TIMESTAMPTZ,
    token_hash TEXT UNIQUE,
    token_expires_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE updater_extension_access OWNER TO mserp_app;
    END IF;
END $$;
INSERT INTO payroll_cache_revisions(table_name) VALUES ('updater_extension_access');
CREATE TRIGGER payroll_cache_insert AFTER INSERT ON updater_extension_access REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON updater_extension_access REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_delete AFTER DELETE ON updater_extension_access REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON updater_extension_access FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
COMMIT;
