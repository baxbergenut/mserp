BEGIN;

-- Production migrations run as postgres; runtime queries run as mserp_app.
-- Match existing application tables without granting access to other roles.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        ALTER TABLE fleetscope_driver_intake OWNER TO mserp_app;
        ALTER TABLE fleetscope_webhook_receipts OWNER TO mserp_app;
        ALTER TABLE relay_identity_reviews OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
