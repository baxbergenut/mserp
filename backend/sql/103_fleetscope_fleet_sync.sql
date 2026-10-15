BEGIN;
-- Local capture is always durable. Credentials and network enablement are never stored here.
CREATE TABLE fleetscope_sync_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 source_revision bigint NOT NULL DEFAULT 0,
 company_id uuid,
 environment text CHECK(environment IN ('production','staging')),
 epoch uuid,
 mode text NOT NULL DEFAULT 'off' CHECK(mode IN ('off','reconcile','live','paused')),
 version bigint NOT NULL DEFAULT 0,
 assignment_sequence bigint NOT NULL DEFAULT 0,
 assignment_hash text NOT NULL DEFAULT '',
 last_error text NOT NULL DEFAULT '',
 last_checked_at timestamptz,
 last_verified_at timestamptz,
 activated_at timestamptz
);
INSERT INTO fleetscope_sync_state(singleton) VALUES(true);
CREATE TABLE fleetscope_driver_identity (
 company_id uuid NOT NULL,
 fleetscope_driver_id uuid NOT NULL,
 driver_id uuid NOT NULL,
 source text NOT NULL CHECK(source IN ('intake','review')),
 confirmed boolean NOT NULL DEFAULT true,
 reviewed_by uuid,
 reviewed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,fleetscope_driver_id),
 UNIQUE(company_id,driver_id)
);
-- Ambiguous old intake links remain review items rather than choosing a winner.
INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,reviewed_by)
SELECT i.company_id,i.fleetscope_driver_id,i.driver_id,'intake',i.completed_by
FROM fleetscope_driver_intake i WHERE i.completed_at IS NOT NULL AND i.driver_id IS NOT NULL
AND (SELECT count(*) FROM fleetscope_driver_intake x WHERE x.company_id=i.company_id AND x.driver_id=i.driver_id AND x.completed_at IS NOT NULL)=1;
CREATE TABLE fleetscope_sync_links (
 kind text NOT NULL CHECK(kind IN ('driver','truck')),
 local_id uuid NOT NULL,
 company_id uuid NOT NULL,
 environment text NOT NULL,
 remote_id uuid,
 remote_revision text NOT NULL DEFAULT '',
 assignment_revision text NOT NULL DEFAULT '',
 source_revision bigint NOT NULL DEFAULT 0,
 acknowledged jsonb,
 acknowledged_hash text NOT NULL DEFAULT '',
 provisioning jsonb,
 excluded_reason text NOT NULL DEFAULT '',
 last_error text NOT NULL DEFAULT '',
 last_synced_at timestamptz,
 version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(kind,local_id),
 UNIQUE(kind,company_id,environment,remote_id)
);
CREATE TABLE fleetscope_sync_snapshots (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 remote_id uuid NOT NULL,
 company_id uuid NOT NULL,
 environment text NOT NULL,
 epoch uuid NOT NULL,
 source_revision bigint NOT NULL,
 payload jsonb NOT NULL,
 local_payload jsonb NOT NULL,
 created_by uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE fleetscope_sync_operations (
 id uuid PRIMARY KEY,
 entity_key text NOT NULL,
 kind text NOT NULL,
 company_id uuid NOT NULL,
 environment text NOT NULL,
 epoch uuid NOT NULL,
 payload bytea NOT NULL,
 body_sha256 text NOT NULL,
 desired_hash text NOT NULL DEFAULT '',
 bootstrap boolean NOT NULL DEFAULT false,
 review_delivery boolean NOT NULL DEFAULT false,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','applied','blocked','superseded')),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid,
 lease_until timestamptz,
 last_error text NOT NULL DEFAULT '',
 last_http_status integer NOT NULL DEFAULT 0,
 result jsonb,
 created_by uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 applied_at timestamptz
);
CREATE UNIQUE INDEX fleetscope_sync_one_pending ON fleetscope_sync_operations(entity_key) WHERE state IN ('pending','processing','blocked');
CREATE INDEX fleetscope_sync_due ON fleetscope_sync_operations(next_attempt_at) WHERE state IN ('pending','processing');
CREATE FUNCTION fleetscope_sync_immutable_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.entity_key,NEW.kind,NEW.company_id,NEW.environment,NEW.epoch,NEW.payload,NEW.body_sha256,NEW.desired_hash,NEW.bootstrap,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.entity_key,OLD.kind,OLD.company_id,OLD.environment,OLD.epoch,OLD.payload,OLD.body_sha256,OLD.desired_hash,OLD.bootstrap,OLD.created_by,OLD.created_at) THEN
  RAISE EXCEPTION 'FleetScope operation identity and payload are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleetscope_sync_immutable BEFORE UPDATE ON fleetscope_sync_operations FOR EACH ROW EXECUTE FUNCTION fleetscope_sync_immutable_operation();
CREATE TABLE fleetscope_sync_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid,
 action text NOT NULL,
 details jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION fleetscope_sync_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE fleetscope_sync_state SET source_revision=source_revision+1 WHERE singleton;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['trucks','drivers','investors','truck_driver_assignments','driver_teams','truck_ownership_history','fleetscope_driver_intake'] LOOP
 EXECUTE format('CREATE TRIGGER fleetscope_capture AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION fleetscope_sync_capture()',t);
 END LOOP;
END $$;
CREATE FUNCTION fleetscope_identity_from_intake() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.completed_at IS NOT NULL AND NEW.driver_id IS NOT NULL THEN
  IF EXISTS(SELECT 1 FROM fleetscope_driver_identity WHERE company_id=NEW.company_id AND (fleetscope_driver_id=NEW.fleetscope_driver_id OR driver_id=NEW.driver_id) AND (fleetscope_driver_id<>NEW.fleetscope_driver_id OR driver_id<>NEW.driver_id)) THEN
   RAISE EXCEPTION 'FleetScope identity is already linked' USING ERRCODE='23505';
  END IF;
  INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,reviewed_by)
  VALUES(NEW.company_id,NEW.fleetscope_driver_id,NEW.driver_id,'intake',NEW.completed_by)
  ON CONFLICT(company_id,fleetscope_driver_id) DO UPDATE SET confirmed=true;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fleetscope_identity_intake AFTER INSERT OR UPDATE OF completed_at,driver_id ON fleetscope_driver_intake FOR EACH ROW EXECUTE FUNCTION fleetscope_identity_from_intake();
DO $$ DECLARE t text; BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 FOREACH t IN ARRAY ARRAY['fleetscope_sync_state','fleetscope_driver_identity','fleetscope_sync_links','fleetscope_sync_snapshots','fleetscope_sync_operations','fleetscope_sync_audit'] LOOP
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO mserp_app',t);
 END LOOP;
 END IF;
END $$;
COMMIT;
