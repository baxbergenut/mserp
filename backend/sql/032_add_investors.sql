BEGIN;

CREATE TABLE investors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name TEXT NOT NULL CHECK (length(trim(full_name)) BETWEEN 1 AND 200),
    driver_id UUID UNIQUE REFERENCES drivers(id) ON DELETE SET NULL,
    is_company BOOLEAN NOT NULL DEFAULT false,
    email TEXT,
    phone TEXT,
    notes TEXT,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (NOT is_company OR (driver_id IS NULL AND active))
);
CREATE UNIQUE INDEX investors_company_idx ON investors(is_company) WHERE is_company;
INSERT INTO investors(id,full_name,is_company)
VALUES ('00000000-0000-0000-0000-000000000001','MS Express Inc.',true);

-- This is a one-time ownership initialization, not a rule tying ownership to operation.
LOCK TABLE trucks, truck_driver_assignments, drivers IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO investors(full_name,driver_id,email,phone)
SELECT DISTINCT d.full_name,d.id,d.email,d.phone FROM drivers d
JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
WHERE d.is_owner_operator;
ALTER TABLE trucks ADD COLUMN owner_id UUID NOT NULL
    DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES investors(id);
CREATE INDEX trucks_owner_idx ON trucks(owner_id);
UPDATE trucks t SET owner_id=i.id FROM truck_driver_assignments a
JOIN investors i ON i.driver_id=a.driver_id
WHERE a.truck_id=t.id AND a.unassigned_at IS NULL;
UPDATE trucks SET is_company_owned=(owner_id='00000000-0000-0000-0000-000000000001');

CREATE TABLE truck_ownership_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    truck_id UUID REFERENCES trucks(id) ON DELETE SET NULL,
    truck_unit TEXT NOT NULL,
    owner_id UUID NOT NULL REFERENCES investors(id),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unassigned_at TIMESTAMPTZ,
    start_known BOOLEAN NOT NULL DEFAULT true,
    CHECK (unassigned_at IS NULL OR unassigned_at >= assigned_at)
);
CREATE UNIQUE INDEX truck_ownership_current_idx ON truck_ownership_history(truck_id)
    WHERE unassigned_at IS NULL;
CREATE INDEX truck_ownership_owner_idx ON truck_ownership_history(owner_id,assigned_at);
INSERT INTO truck_ownership_history(truck_id,truck_unit,owner_id,start_known)
SELECT id,unit_number,owner_id,false FROM trucks;

CREATE FUNCTION record_truck_ownership() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE changed_at timestamptz := clock_timestamp();
BEGIN
    IF TG_OP='DELETE' THEN
        UPDATE truck_ownership_history SET unassigned_at=changed_at
        WHERE truck_id=OLD.id AND unassigned_at IS NULL;
        RETURN OLD;
    END IF;
    -- Keep the legacy flag consistent even for older clients.
    NEW.is_company_owned := (NEW.owner_id='00000000-0000-0000-0000-000000000001');
    IF TG_OP='UPDATE' AND NEW.owner_id IS NOT DISTINCT FROM OLD.owner_id THEN
        RETURN NEW;
    END IF;
    UPDATE truck_ownership_history SET unassigned_at=changed_at
    WHERE truck_id=NEW.id AND unassigned_at IS NULL;
    INSERT INTO truck_ownership_history(truck_id,truck_unit,owner_id,assigned_at)
    VALUES(NEW.id,NEW.unit_number,NEW.owner_id,changed_at);
    RETURN NEW;
END $$;
-- Deferred FK permits history to be recorded in the BEFORE INSERT trigger.
ALTER TABLE truck_ownership_history ALTER CONSTRAINT truck_ownership_history_truck_id_fkey
    DEFERRABLE INITIALLY DEFERRED;
CREATE TRIGGER trucks_ownership BEFORE INSERT OR UPDATE OR DELETE ON trucks
FOR EACH ROW EXECUTE FUNCTION record_truck_ownership();

-- Preserve the most recent identity if the driver profile is later removed.
CREATE FUNCTION sync_investor_driver_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE investors SET full_name=NEW.full_name,email=NEW.email,phone=NEW.phone,updated_at=now()
    WHERE driver_id=NEW.id;
    RETURN NEW;
END $$;
CREATE TRIGGER investor_driver_identity AFTER UPDATE OF full_name,email,phone ON drivers
FOR EACH ROW EXECUTE FUNCTION sync_investor_driver_identity();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE investors OWNER TO mserp_app;
        ALTER TABLE truck_ownership_history OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
