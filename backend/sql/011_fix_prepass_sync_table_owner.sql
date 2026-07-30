BEGIN;

-- Production migrations run as PostgreSQL's administrative role, while the
-- API connects as mserp_app. Match the ownership of the existing application
-- tables without breaking local databases that do not define that role.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        EXECUTE 'ALTER TABLE prepass_toll_sync_days OWNER TO mserp_app';
    END IF;
END
$$;

COMMIT;
