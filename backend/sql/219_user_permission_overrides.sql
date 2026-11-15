-- User exceptions are applied after role grants; missing keys inherit.
ALTER TABLE app_users ADD COLUMN permission_overrides jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(permission_overrides)='object');
ALTER TABLE app_users ADD COLUMN expense_category_overrides jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(expense_category_overrides)='object');

-- Used only for capabilities without prerequisites (access management and board presence).
CREATE FUNCTION user_permission_allowed(role_permissions text[], overrides jsonb, permission_key text) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE overrides->>permission_key WHEN 'allow' THEN true WHEN 'deny' THEN false ELSE permission_key=ANY(role_permissions) END
$$;
