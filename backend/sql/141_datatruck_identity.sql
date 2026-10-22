BEGIN;
ALTER TABLE drivers ADD COLUMN date_of_birth date CHECK (date_of_birth >= DATE '1900-01-01');
-- Provider identity is distinct from local UUIDs and from a successful HTTP response.
ALTER TABLE datatruck_enrollments ADD COLUMN provider_id bigint CHECK (provider_id > 0);
ALTER TABLE datatruck_enrollments ADD COLUMN provider_carrier_id integer CHECK (provider_carrier_id > 0);
ALTER TABLE datatruck_enrollments ADD COLUMN profile_baseline jsonb;
ALTER TABLE datatruck_enrollments ADD COLUMN identity_evidence jsonb;
UPDATE datatruck_enrollments SET provider_id=(receipt->>'id')::bigint
 WHERE state='done' AND jsonb_typeof(receipt->'id')='number'
 AND receipt->>'id' ~ '^[0-9]{1,18}$' AND (receipt->>'id')::bigint > 0;
CREATE UNIQUE INDEX datatruck_provider_identity ON datatruck_enrollments(environment,entity_kind,provider_id)
 WHERE provider_id IS NOT NULL;
CREATE FUNCTION datatruck_driver_profile(d drivers) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('first_name',to_jsonb(d)->'first_name','last_name',to_jsonb(d)->'last_name','email',d.email,
 'contact_number',CASE WHEN d.phone IS NULL THEN NULL ELSE '+1'||d.phone END,'brith_date',d.date_of_birth,
 'driver_license',d.license_number,'driver_license_state',d.license_state,'driver_license_expiration',d.license_expires)
$$;
CREATE TABLE datatruck_driver_updates (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 driver_id uuid NOT NULL,
 provider_id bigint NOT NULL CHECK(provider_id>0), environment text NOT NULL, carrier_id integer NOT NULL,
 source_snapshot jsonb NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','review','unconfirmed','accepted','superseded','canceled')),
 request_payload jsonb, receipt jsonb, last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz
);
CREATE TABLE datatruck_driver_update_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, update_id bigint NOT NULL,
 snapshot jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION log_datatruck_driver_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 INSERT INTO datatruck_driver_update_events(update_id,snapshot) VALUES(NEW.id,to_jsonb(NEW)); RETURN NEW;
END $$;
CREATE TRIGGER datatruck_driver_update_log AFTER INSERT OR UPDATE ON datatruck_driver_updates
 FOR EACH ROW EXECUTE FUNCTION log_datatruck_driver_update();
CREATE FUNCTION queue_datatruck_driver_update() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE link datatruck_enrollments; profile jsonb;
BEGIN
 IF current_setting('mserp.datatruck_import',true)='on' THEN RETURN NEW; END IF;
 profile=datatruck_driver_profile(NEW);
 IF profile IS NOT DISTINCT FROM datatruck_driver_profile(OLD) THEN RETURN NEW; END IF;
 SELECT * INTO link FROM datatruck_enrollments WHERE entity_kind='drivers' AND entity_id=NEW.id;
 IF link.provider_id IS NULL OR link.state<>'done' OR link.profile_baseline IS NULL OR link.provider_carrier_id IS NULL THEN RETURN NEW; END IF;
 UPDATE datatruck_driver_updates SET state='superseded',updated_at=now()
 WHERE driver_id=NEW.id AND state IN ('queued','review');
 INSERT INTO datatruck_driver_updates(driver_id,provider_id,environment,carrier_id,source_snapshot)
 VALUES(NEW.id,link.provider_id,link.environment,link.provider_carrier_id,profile);
 RETURN NEW;
END $$;
CREATE TRIGGER datatruck_driver_profile_update AFTER UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION queue_datatruck_driver_update();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE datatruck_driver_updates OWNER TO mserp_app;
  ALTER TABLE datatruck_driver_update_events OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
