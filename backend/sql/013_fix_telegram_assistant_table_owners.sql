BEGIN;

-- Production migrations run as PostgreSQL's administrative role, while the
-- API connects as mserp_app. Transfer the Telegram assistant tables to the
-- application role so its durable workers and authenticated settings routes
-- can read and write them.
DO $$
DECLARE
    table_name TEXT;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mserp_app') THEN
        FOREACH table_name IN ARRAY ARRAY[
            'telegram_managers',
            'telegram_identities',
            'telegram_link_tokens',
            'telegram_updates',
            'assistant_conversations',
            'assistant_action_requests',
            'assistant_audit_log',
            'assistant_action_audit'
        ]
        LOOP
            EXECUTE format('ALTER TABLE %I OWNER TO mserp_app', table_name);
        END LOOP;
    END IF;
END
$$;

COMMIT;
