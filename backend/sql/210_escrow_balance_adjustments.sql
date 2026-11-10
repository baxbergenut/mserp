BEGIN;

-- Ordinary releases are positive outflows. An audited correction can also be
-- a negative outflow, which adds to the held balance without a payroll credit.
ALTER TABLE driver_escrow_releases
 DROP CONSTRAINT driver_escrow_releases_check,
 ADD CONSTRAINT escrow_balance_movement_check CHECK (
   (correction_reason IS NOT NULL AND amount<>0)
   OR (correction_reason IS NULL AND (
     (payroll_return IS NULL AND amount>0 AND collection_stopped IS NULL)
     OR (payroll_return IS NOT NULL AND payroll_return BETWEEN 0 AND amount AND (amount>0 OR collection_stopped IS NOT NULL))
   ))
 );

CREATE OR REPLACE FUNCTION escrow_collection_available(target uuid, target_week date) RETURNS numeric
LANGUAGE sql STABLE AS $$
    SELECT min(e.amount-e.opening_paid
        -coalesce((SELECT sum(p.amount) FROM driver_escrow_payments p
          WHERE p.escrow_id=target AND p.week_start<=w.week_start AND p.week_start<>target_week),0)
        +coalesce((SELECT sum(r.amount) FROM driver_escrow_releases r
          WHERE r.escrow_id=target AND NOT r.cancelled
            AND (r.week_start<w.week_start OR (w.after_movements AND r.week_start=w.week_start))),0))
    FROM driver_escrows e CROSS JOIN (
        SELECT target_week week_start,false after_movements
        UNION
        SELECT week_start,false FROM driver_escrow_payments WHERE escrow_id=target AND week_start>target_week
        UNION
        SELECT week_start,true FROM driver_escrow_releases
          WHERE escrow_id=target AND NOT cancelled AND correction_reason IS NOT NULL AND amount<0 AND week_start>=target_week
    ) w WHERE e.id=target
$$;

CREATE OR REPLACE FUNCTION assert_escrow_collection_limit(target uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM driver_escrows WHERE id=target FOR UPDATE;
    IF EXISTS (SELECT 1 FROM driver_escrow_payments p WHERE p.escrow_id=target
        AND p.amount>escrow_collection_available(target,p.week_start)) THEN
        RAISE EXCEPTION 'Escrow repayments exceed the required balance; review later repayments before changing the balance' USING ERRCODE='23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM driver_escrows e JOIN driver_escrow_releases a ON a.escrow_id=e.id
        WHERE e.id=target AND NOT a.cancelled AND a.correction_reason IS NOT NULL AND a.amount<0
          AND e.opening_paid
              +coalesce((SELECT sum(p.amount) FROM driver_escrow_payments p
                WHERE p.escrow_id=target AND p.week_start<=a.week_start),0)
              -coalesce((SELECT sum(r.amount) FROM driver_escrow_releases r
                WHERE r.escrow_id=target AND NOT r.cancelled AND r.week_start<=a.week_start),0)>e.amount
    ) THEN
        RAISE EXCEPTION 'The correction exceeds the required escrow balance in its effective week' USING ERRCODE='23514';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION protect_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver uuid; current_week date := date_trunc('week',now() AT TIME ZONE 'America/New_York')::date;
BEGIN
 IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'Cancel escrow releases instead of deleting history' USING ERRCODE='23514';
 END IF;
 SELECT driver_id INTO driver FROM driver_escrows WHERE id=NEW.escrow_id FOR UPDATE;
 IF TG_OP='UPDATE' THEN
    IF NEW.id<>OLD.id OR NEW.escrow_id<>OLD.escrow_id OR OLD.cancelled
      OR NEW.payroll_return IS DISTINCT FROM OLD.payroll_return OR NEW.collection_stopped IS DISTINCT FROM OLD.collection_stopped
      OR (OLD.payroll_return IS NULL AND OLD.week_start<current_week)
      OR (OLD.payroll_return IS NOT NULL AND (NOT NEW.cancelled OR NEW.week_start<>OLD.week_start OR NEW.amount<>OLD.amount)) THEN
        RAISE EXCEPTION 'This release can no longer be changed' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=driver AND week_start=OLD.week_start AND finalized) THEN
        RAISE EXCEPTION 'Reopen this driver settlement before changing its release' USING ERRCODE='23514';
    END IF;
    NEW.version := OLD.version+1;
 END IF;
 IF (driver IS NULL AND NEW.correction_reason IS NULL)
    OR (NEW.correction_reason IS NOT NULL AND NEW.week_start<>current_week)
    OR (NEW.payroll_return IS NULL AND NEW.week_start<current_week)
    OR (NEW.payroll_return IS NOT NULL AND NEW.week_start>=current_week) THEN
    RAISE EXCEPTION 'Invalid driver or week for this escrow transaction' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM payroll_settlements WHERE driver_id=driver AND week_start=NEW.week_start AND finalized) THEN
    RAISE EXCEPTION 'Reopen this driver settlement before changing its release' USING ERRCODE='23514';
 END IF;
 UPDATE driver_escrows SET balance_version=balance_version+1,updated_at=now() WHERE id=NEW.escrow_id;
 NEW.updated_at := now();
 RETURN NEW;
END $$;

COMMIT;
