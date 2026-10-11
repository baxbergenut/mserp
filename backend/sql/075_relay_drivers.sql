BEGIN;
CREATE TABLE relay_driver_rosters (
    environment TEXT PRIMARY KEY CHECK (environment IN ('production','staging')),
    snapshot JSONB NOT NULL,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE relay_driver_operations (
    id UUID PRIMARY KEY,
    environment TEXT NOT NULL CHECK (environment IN ('production','staging')),
    target TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('add','edit','remove','enable','disable','assign','unassign')),
    relay_driver_id TEXT NOT NULL DEFAULT '',
    driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
    policy_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','confirmed','failed','verified')),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX relay_driver_operation_pending ON relay_driver_operations(environment,target) WHERE state='pending';
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE relay_driver_rosters OWNER TO mserp_app;
  ALTER TABLE relay_driver_operations OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
