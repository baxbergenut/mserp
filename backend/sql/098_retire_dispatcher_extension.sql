BEGIN;

-- Retire MSERP-issued credentials. Refresher Orb owns separate accounts and data.
-- Preserve access audit history and record remaining credentials before removal.
DO $$ BEGIN
    IF to_regclass('dispatcher_extension_access') IS NOT NULL THEN
        INSERT INTO access_audit(action,target_id,details)
        SELECT 'dispatcher_extension_retired',dispatcher_id,
            '{"reason":"Moved to standalone Refresher Orb","enabled":false}'::jsonb
        FROM dispatcher_extension_access
        WHERE enabled OR activation_hash IS NOT NULL OR token_hash IS NOT NULL;
    END IF;
    IF to_regclass('updater_extension_access') IS NOT NULL THEN
        INSERT INTO access_audit(action,target_id,details)
        SELECT 'updater_extension_retired',updater_id,
            '{"reason":"Moved to standalone Refresher Orb","enabled":false}'::jsonb
        FROM updater_extension_access
        WHERE enabled OR activation_hash IS NOT NULL OR token_hash IS NOT NULL;
    END IF;
END $$;

DROP TRIGGER IF EXISTS dispatcher_extension_deactivation ON dispatchers;
DROP FUNCTION IF EXISTS revoke_inactive_dispatcher_extension();
DROP TABLE IF EXISTS dispatcher_extension_access;
DROP TABLE IF EXISTS updater_extension_access;
DELETE FROM payroll_cache_revisions
WHERE table_name IN ('dispatcher_extension_access','updater_extension_access','dispatcher_extension_events');

COMMIT;
