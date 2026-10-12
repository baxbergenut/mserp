BEGIN;
CREATE TABLE prepass_fleet_rosters (
 environment TEXT PRIMARY KEY CHECK(environment IN ('production','nonproduction')),
 snapshot JSONB NOT NULL,
 refreshed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE prepass_fleet_links (
 environment TEXT NOT NULL CHECK(environment IN ('production','nonproduction')),
 account_number TEXT NOT NULL,
 vin TEXT NOT NULL,
 truck_id UUID NOT NULL REFERENCES trucks(id) ON DELETE CASCADE,
 updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(environment,account_number,vin),
 UNIQUE(environment,truck_id)
);
CREATE TABLE prepass_fleet_operations (
 id UUID PRIMARY KEY,
 environment TEXT NOT NULL CHECK(environment IN ('production','nonproduction')),
 account_number TEXT NOT NULL,
 vin TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('add','edit','remove','assign','link')),
 request_hash TEXT NOT NULL,
 request JSONB NOT NULL,
 before_vehicle JSONB,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','confirmed','failed','verified')),
 actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 resolved_at TIMESTAMPTZ,
 resolved_by UUID REFERENCES app_users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX prepass_fleet_pending ON prepass_fleet_operations(environment) WHERE state='pending';
CREATE INDEX prepass_fleet_operation_history ON prepass_fleet_operations(environment,created_at DESC);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE prepass_fleet_rosters OWNER TO mserp_app;
  ALTER TABLE prepass_fleet_links OWNER TO mserp_app;
  ALTER TABLE prepass_fleet_operations OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
