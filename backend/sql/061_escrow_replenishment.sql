BEGIN;

-- A release reopens collection capacity starting the following week. Reserve
-- already saved later payments so editing an earlier week cannot overcollect.
CREATE FUNCTION escrow_collection_available(target uuid, target_week date) RETURNS numeric
LANGUAGE sql STABLE AS $$
    SELECT min(e.amount-e.opening_paid
        -coalesce((SELECT sum(p.amount) FROM driver_escrow_payments p
          WHERE p.escrow_id=target AND p.week_start<=w.week_start AND p.week_start<>target_week),0)
        +coalesce((SELECT sum(r.amount) FROM driver_escrow_releases r
          WHERE r.escrow_id=target AND NOT r.cancelled AND r.week_start<w.week_start),0))
    FROM driver_escrows e CROSS JOIN (
        SELECT target_week week_start UNION
        SELECT week_start FROM driver_escrow_payments WHERE escrow_id=target AND week_start>target_week
    ) w WHERE e.id=target
$$;

CREATE FUNCTION assert_escrow_collection_limit(target uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS (SELECT 1 FROM driver_escrow_payments p WHERE p.escrow_id=target
        AND p.amount>escrow_collection_available(target,p.week_start)) THEN
        RAISE EXCEPTION 'Escrow repayments exceed the required balance; adjust later repayments before changing this release' USING ERRCODE='23514';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION protect_escrow_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e driver_escrows%ROWTYPE; target uuid; target_week date;
BEGIN
    IF TG_OP='DELETE' THEN target:=OLD.escrow_id; target_week:=OLD.week_start;
    ELSE target:=NEW.escrow_id; target_week:=NEW.week_start; END IF;
    IF TG_OP='UPDATE' AND (NEW.escrow_id<>OLD.escrow_id OR NEW.week_start<>OLD.week_start) THEN
        RAISE EXCEPTION 'Escrow payment identity cannot change' USING ERRCODE='23514';
    END IF;
    SELECT * INTO e FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=e.driver_id AND week_start=target_week AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before editing escrow' USING ERRCODE='23514';
    END IF;
    IF TG_OP<>'DELETE' THEN
        IF NEW.amount>escrow_collection_available(target,target_week)
          OR (target_week+6<e.start_date AND NOT EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id=target AND week_start=target_week)) THEN
            RAISE EXCEPTION 'Escrow payment exceeds the available balance or precedes its start' USING ERRCODE='23514';
        END IF;
    END IF;
    UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=target;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION audit_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_escrow_release_funding(NEW.escrow_id);
    PERFORM assert_escrow_collection_limit(NEW.escrow_id);
    INSERT INTO driver_escrow_release_events(release_id,actor_id,before_value,after_value)
    VALUES(NEW.id,NEW.updated_by,CASE WHEN TG_OP='UPDATE' THEN to_jsonb(OLD) ELSE NULL END,to_jsonb(NEW));
    RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION protect_released_escrow_funds() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid;
BEGIN
    IF TG_TABLE_NAME='driver_escrows' THEN target:=NEW.id;
    ELSIF TG_OP='DELETE' THEN target:=OLD.escrow_id;
    ELSE target:=NEW.escrow_id; END IF;
    PERFORM assert_escrow_release_funding(target);
    PERFORM assert_escrow_collection_limit(target);
    RETURN NULL;
END $$;
DROP TRIGGER escrow_opening_release_funds ON driver_escrows;
CREATE TRIGGER escrow_opening_release_funds AFTER UPDATE OF opening_paid,amount ON driver_escrows
FOR EACH ROW EXECUTE FUNCTION protect_released_escrow_funds();

COMMIT;
