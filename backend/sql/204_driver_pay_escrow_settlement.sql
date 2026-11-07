BEGIN;

ALTER TABLE driver_pay_weeks
 ADD COLUMN manually_added boolean NOT NULL DEFAULT false,
 ADD COLUMN escrow_draft jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE expense_payments
 ADD COLUMN escrow_amount numeric(14,2) NOT NULL DEFAULT 0 CHECK (escrow_amount BETWEEN 0 AND amount);
ALTER TABLE driver_escrow_releases
 ADD COLUMN payroll_return numeric(14,2),
 ADD COLUMN collection_stopped boolean,
 DROP CONSTRAINT driver_escrow_releases_amount_check,
 ADD CHECK ((payroll_return IS NULL AND amount>0 AND collection_stopped IS NULL)
 OR (payroll_return IS NOT NULL AND payroll_return BETWEEN 0 AND amount AND (amount>0 OR collection_stopped IS NOT NULL)));
CREATE UNIQUE INDEX escrow_payroll_posting_week_idx ON driver_escrow_releases(escrow_id,week_start)
 WHERE NOT cancelled AND payroll_return IS NOT NULL;

CREATE FUNCTION escrow_collection_stopped(target uuid, target_week date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT coalesce((SELECT r.collection_stopped FROM driver_escrow_releases r
 WHERE r.escrow_id=target AND r.week_start<=target_week AND (r.collection_stopped OR r.week_start<target_week) AND NOT r.cancelled AND r.collection_stopped IS NOT NULL
 ORDER BY r.week_start DESC LIMIT 1),false)
$$;

CREATE FUNCTION escrow_payroll_suppressed(target uuid, driver uuid, target_week date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT coalesce((SELECT (w.escrow_draft->>'stopCollection')::boolean FROM driver_pay_weeks w
 WHERE (w.escrow_draft->>'stopCollection')::boolean AND w.driver_id=driver AND w.week_start=target_week AND w.escrow_draft->>'escrowId'=target::text),
 escrow_collection_stopped(target,target_week))
$$;

-- Posted allocations share the legacy funding and audit triggers. They may use
-- completed weeks and can only be cancelled by reopening their own statement.
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
 IF driver IS NULL OR (NEW.payroll_return IS NULL AND NEW.week_start<current_week)
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

CREATE FUNCTION guard_stopped_escrow_payment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.amount>0 AND escrow_collection_stopped(NEW.escrow_id,NEW.week_start)
    AND (TG_OP='INSERT' OR NEW.amount>OLD.amount) THEN
    RAISE EXCEPTION 'Escrow collection is stopped for this week' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER escrow_collection_policy_guard BEFORE INSERT OR UPDATE ON driver_escrow_payments
 FOR EACH ROW EXECUTE FUNCTION guard_stopped_escrow_payment();

COMMIT;
