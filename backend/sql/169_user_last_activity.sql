BEGIN;

-- Durable activity survives logout, revocation and expired-session cleanup.
-- The existing activity table is excluded from financial cache revisions.
ALTER TABLE user_login_activity ADD COLUMN last_activity_at TIMESTAMPTZ;
UPDATE user_login_activity a SET last_activity_at=GREATEST(
    a.last_login_at,
    (SELECT max(s.last_seen_at) FROM auth_sessions s WHERE s.user_id=a.user_id)
);

COMMIT;
