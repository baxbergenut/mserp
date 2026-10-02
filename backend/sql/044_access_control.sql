BEGIN;

CREATE TABLE app_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 80),
    permissions TEXT[] NOT NULL DEFAULT '{}',
    system_role BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX app_roles_name_idx ON app_roles(lower(name));
CREATE UNIQUE INDEX app_roles_system_idx ON app_roles(system_role) WHERE system_role;
INSERT INTO app_roles(name, system_role) VALUES ('Administrator', true);

ALTER TABLE app_users ADD COLUMN email TEXT;
ALTER TABLE app_users ADD COLUMN role_id UUID REFERENCES app_roles(id);
ALTER TABLE app_users ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE app_users ADD CONSTRAINT app_users_email_check CHECK
    (email IS NULL OR (email = lower(btrim(email)) AND email ~ '^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$'));
CREATE UNIQUE INDEX app_users_email_idx ON app_users(lower(email)) WHERE email IS NOT NULL;
-- Existing users already had unrestricted access. Preserve it explicitly.
-- Legacy username login works only until an administrator attaches a real email.
UPDATE app_users SET role_id=(SELECT id FROM app_roles WHERE system_role);

CREATE TABLE access_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_id UUID NOT NULL,
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DELETE FROM auth_sessions;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE app_roles OWNER TO mserp_app;
        ALTER TABLE access_audit OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
