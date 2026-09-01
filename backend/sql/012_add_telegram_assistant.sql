BEGIN;

CREATE TABLE telegram_managers (
    app_user_id UUID PRIMARY KEY REFERENCES app_users(id) ON DELETE CASCADE,
    approved_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE telegram_identities (
    app_user_id UUID PRIMARY KEY REFERENCES telegram_managers(app_user_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL UNIQUE,
    telegram_chat_id BIGINT NOT NULL UNIQUE,
    telegram_username TEXT,
    display_name TEXT,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > linked_at)
);

CREATE INDEX telegram_identities_expires_at_idx ON telegram_identities (expires_at);

CREATE TABLE telegram_link_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_user_id UUID NOT NULL REFERENCES telegram_managers(app_user_id) ON DELETE CASCADE,
    token_hash CHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX telegram_link_tokens_user_idx ON telegram_link_tokens (app_user_id, created_at DESC);
CREATE INDEX telegram_link_tokens_expires_idx ON telegram_link_tokens (expires_at) WHERE used_at IS NULL;

CREATE TABLE telegram_updates (
    update_id BIGINT PRIMARY KEY,
    telegram_user_id BIGINT,
    telegram_chat_id BIGINT,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'processing', 'completed', 'failed', 'rejected')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX telegram_updates_queue_idx
    ON telegram_updates (next_attempt_at, received_at)
    WHERE status IN ('queued', 'failed');

CREATE TABLE assistant_conversations (
    app_user_id UUID PRIMARY KEY REFERENCES telegram_managers(app_user_id) ON DELETE CASCADE,
    messages JSONB NOT NULL DEFAULT '[]'::jsonb,
    context JSONB NOT NULL DEFAULT '{}'::jsonb,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX assistant_conversations_expires_idx ON assistant_conversations (expires_at);

CREATE TABLE assistant_action_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_user_id UUID NOT NULL REFERENCES telegram_managers(app_user_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    telegram_chat_id BIGINT NOT NULL,
    action_name TEXT NOT NULL,
    arguments JSONB NOT NULL,
    before_state JSONB,
    before_hash CHAR(64),
    preview TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'executing', 'confirmed', 'cancelled', 'expired', 'failed')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX assistant_action_requests_pending_idx
    ON assistant_action_requests (app_user_id, expires_at)
    WHERE status = 'pending';

CREATE TABLE assistant_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_user_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    telegram_update_id BIGINT,
    event_type TEXT NOT NULL,
    prompt TEXT,
    response TEXT,
    tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb,
    tool_results JSONB NOT NULL DEFAULT '[]'::jsonb,
    outcome TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '30 days')
);

CREATE INDEX assistant_audit_log_user_created_idx
    ON assistant_audit_log (app_user_id, created_at DESC);
CREATE INDEX assistant_audit_log_expires_idx ON assistant_audit_log (expires_at);

CREATE TABLE assistant_action_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_user_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    action_request_id UUID REFERENCES assistant_action_requests(id) ON DELETE SET NULL,
    action_name TEXT NOT NULL,
    arguments JSONB NOT NULL,
    before_state JSONB,
    after_state JSONB,
    outcome TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '1 year')
);

CREATE INDEX assistant_action_audit_user_created_idx
    ON assistant_action_audit (app_user_id, created_at DESC);
CREATE INDEX assistant_action_audit_expires_idx ON assistant_action_audit (expires_at);

COMMIT;
