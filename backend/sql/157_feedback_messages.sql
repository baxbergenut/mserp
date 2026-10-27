BEGIN;
CREATE TABLE feedback_messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 post_id uuid NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
 recipient_id uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
 post_version integer NOT NULL,
 title text NOT NULL,
 completed_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 read_at timestamptz,
 UNIQUE(post_id,post_version)
);
CREATE INDEX feedback_messages_recipient ON feedback_messages(recipient_id,created_at DESC,id);
CREATE INDEX feedback_messages_unread ON feedback_messages(recipient_id) WHERE read_at IS NULL;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE feedback_messages OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
