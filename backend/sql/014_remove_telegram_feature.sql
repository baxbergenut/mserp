BEGIN;

DROP TABLE IF EXISTS assistant_action_audit;
DROP TABLE IF EXISTS assistant_audit_log;
DROP TABLE IF EXISTS assistant_action_requests;
DROP TABLE IF EXISTS assistant_conversations;
DROP TABLE IF EXISTS telegram_updates;
DROP TABLE IF EXISTS telegram_link_tokens;
DROP TABLE IF EXISTS telegram_identities;
DROP TABLE IF EXISTS telegram_managers;

COMMIT;
