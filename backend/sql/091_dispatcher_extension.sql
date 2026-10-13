BEGIN;
CREATE TABLE dispatcher_extension_access (
    dispatcher_id UUID PRIMARY KEY REFERENCES dispatchers(id) ON DELETE CASCADE,
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
CREATE FUNCTION revoke_inactive_dispatcher_extension() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.active AND NOT NEW.active THEN
        UPDATE dispatcher_extension_access SET enabled=false,activation_hash=NULL,
            activation_expires_at=NULL,token_hash=NULL,token_expires_at=NULL,
            version=version+1,updated_at=now() WHERE dispatcher_id=NEW.id;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER dispatcher_extension_deactivation AFTER UPDATE OF active ON dispatchers
FOR EACH ROW EXECUTE FUNCTION revoke_inactive_dispatcher_extension();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE dispatcher_extension_access OWNER TO mserp_app;
    END IF;
END $$;
INSERT INTO payroll_cache_revisions(table_name) VALUES ('dispatcher_extension_access');
CREATE TRIGGER payroll_cache_insert AFTER INSERT ON dispatcher_extension_access REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_update AFTER UPDATE ON dispatcher_extension_access REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_delete AFTER DELETE ON dispatcher_extension_access REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON dispatcher_extension_access FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
COMMIT;
