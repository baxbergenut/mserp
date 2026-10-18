BEGIN;

-- Keep login history after sessions expire or are revoked. Activity deliberately
-- lives outside app_users so heartbeats do not invalidate access/payroll caches.
CREATE TABLE user_login_activity (
    user_id UUID PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    last_login_at TIMESTAMPTZ NOT NULL
);
INSERT INTO user_login_activity(user_id,last_login_at)
SELECT user_id,max(created_at) FROM auth_sessions GROUP BY user_id;

-- Existing sessions must send a browser heartbeat before being considered online.
ALTER TABLE auth_sessions ADD COLUMN last_seen_at TIMESTAMPTZ;

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE user_login_activity OWNER TO mserp_app;
    END IF;
END $$;

COMMIT;
