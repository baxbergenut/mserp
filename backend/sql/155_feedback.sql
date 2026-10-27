BEGIN;
CREATE TABLE feedback_posts (
 id uuid PRIMARY KEY,
 author_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 author_name text NOT NULL,
 title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
 body text NOT NULL CHECK (length(body) BETWEEN 1 AND 10000),
 tags text[] NOT NULL DEFAULT '{}',
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','completed','discarded')),
 version integer NOT NULL DEFAULT 1,
 submission_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_posts_feed ON feedback_posts(created_at DESC,id);
CREATE INDEX feedback_posts_status ON feedback_posts(status,created_at DESC);
CREATE INDEX feedback_posts_tags ON feedback_posts USING gin(tags);
CREATE TABLE feedback_votes (
 post_id uuid NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
 PRIMARY KEY(post_id,user_id)
);
CREATE TABLE feedback_comments (
 id uuid PRIMARY KEY,
 post_id uuid NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
 author_id uuid REFERENCES app_users(id) ON DELETE SET NULL,
 author_name text NOT NULL,
 body text NOT NULL CHECK (length(body) BETWEEN 1 AND 5000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_comments_post ON feedback_comments(post_id,created_at,id);
CREATE TABLE feedback_attachments (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 post_id uuid NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
 file_name text NOT NULL,
 content_type text NOT NULL,
 data bytea NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 10485760),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_attachments_post ON feedback_attachments(post_id);
CREATE TABLE feedback_status_history (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 post_id uuid NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
 actor_name text NOT NULL,
 old_status text NOT NULL,
 new_status text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_history_post ON feedback_status_history(post_id,created_at,id);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE feedback_posts OWNER TO mserp_app;
  ALTER TABLE feedback_votes OWNER TO mserp_app;
  ALTER TABLE feedback_comments OWNER TO mserp_app;
  ALTER TABLE feedback_attachments OWNER TO mserp_app;
  ALTER TABLE feedback_status_history OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
