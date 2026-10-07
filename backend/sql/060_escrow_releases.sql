BEGIN;

CREATE TABLE driver_escrow_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    escrow_id UUID NOT NULL REFERENCES driver_escrows(id) ON DELETE RESTRICT,
    week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    cancelled BOOLEAN NOT NULL DEFAULT false,
    version INTEGER NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES app_users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_escrow_releases_escrow_idx ON driver_escrow_releases(escrow_id,week_start);
CREATE INDEX driver_escrow_releases_week_idx ON driver_escrow_releases(week_start) WHERE NOT cancelled;
CREATE TABLE driver_escrow_release_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id UUID NOT NULL REFERENCES driver_escrow_releases(id),
    actor_id UUID REFERENCES app_users(id) ON DELETE SET NULL,
    before_value JSONB,
    after_value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- All scheduled credits reserve funds, and none may spend a later collection.
CREATE FUNCTION assert_escrow_release_funding(target uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE initial_paid numeric;
BEGIN
    SELECT opening_paid INTO initial_paid FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM driver_escrow_releases r WHERE r.escrow_id=target AND NOT r.cancelled
        AND (SELECT coalesce(sum(x.amount),0) FROM driver_escrow_releases x WHERE x.escrow_id=target AND NOT x.cancelled AND x.week_start<=r.week_start)
          > initial_paid+(SELECT coalesce(sum(p.amount),0) FROM driver_escrow_payments p WHERE p.escrow_id=target AND p.week_start<=r.week_start)
    ) THEN
        RAISE EXCEPTION 'Release exceeds escrow funds available in the selected week' USING ERRCODE='23514';
    END IF;
END $$;

CREATE FUNCTION protect_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver uuid; current_week date := date_trunc('week',now() AT TIME ZONE 'America/New_York')::date;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'Cancel escrow releases instead of deleting history' USING ERRCODE='23514';
    END IF;
    SELECT driver_id INTO driver FROM driver_escrows WHERE id=NEW.escrow_id FOR UPDATE;
    IF TG_OP='UPDATE' THEN
        IF NEW.id<>OLD.id OR NEW.escrow_id<>OLD.escrow_id OR OLD.cancelled OR OLD.week_start<current_week THEN
            RAISE EXCEPTION 'This release can no longer be changed' USING ERRCODE='23514';
        END IF;
        IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=driver AND week_start=OLD.week_start AND finalized) THEN
            RAISE EXCEPTION 'Reopen this driver settlement before changing its release' USING ERRCODE='23514';
        END IF;
        NEW.version := OLD.version+1;
    END IF;
    IF driver IS NULL OR NEW.week_start<current_week THEN
        RAISE EXCEPTION 'Select an existing driver and the current or a future week' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=driver AND week_start=NEW.week_start AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before changing its release' USING ERRCODE='23514';
    END IF;
    UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=NEW.escrow_id;
    NEW.updated_at := now();
    RETURN NEW;
END $$;
CREATE TRIGGER escrow_release_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_releases
FOR EACH ROW EXECUTE FUNCTION protect_escrow_release();

CREATE FUNCTION audit_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_escrow_release_funding(NEW.escrow_id);
    INSERT INTO driver_escrow_release_events(release_id,actor_id,before_value,after_value)
    VALUES(NEW.id,NEW.updated_by,CASE WHEN TG_OP='UPDATE' THEN to_jsonb(OLD) ELSE NULL END,to_jsonb(NEW));
    RETURN NULL;
END $$;
CREATE TRIGGER escrow_release_audit AFTER INSERT OR UPDATE ON driver_escrow_releases
FOR EACH ROW EXECUTE FUNCTION audit_escrow_release();

CREATE FUNCTION protect_released_escrow_funds() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='driver_escrows' THEN PERFORM assert_escrow_release_funding(NEW.id);
    ELSIF TG_OP='DELETE' THEN PERFORM assert_escrow_release_funding(OLD.escrow_id);
    ELSE PERFORM assert_escrow_release_funding(NEW.escrow_id); END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER escrow_payment_release_funds AFTER INSERT OR UPDATE OR DELETE ON driver_escrow_payments
FOR EACH ROW EXECUTE FUNCTION protect_released_escrow_funds();
CREATE TRIGGER escrow_opening_release_funds AFTER UPDATE OF opening_paid ON driver_escrows
FOR EACH ROW EXECUTE FUNCTION protect_released_escrow_funds();

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_escrow_releases OWNER TO mserp_app;
        ALTER TABLE driver_escrow_release_events OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
