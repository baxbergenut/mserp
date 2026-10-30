BEGIN;

-- Retire remembered-device sessions without granting a fresh login window.
-- Preserve shorter expirations and original sign-in/activity timestamps.
UPDATE auth_sessions
SET expires_at = created_at + interval '8 hours'
WHERE expires_at > created_at + interval '8 hours';

COMMIT;
