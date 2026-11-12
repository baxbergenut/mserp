BEGIN;

-- Accounts retain their original investor when a truck changes owners.
ALTER TABLE driver_escrows ADD COLUMN truck_id uuid REFERENCES trucks(id) ON DELETE RESTRICT,
 ADD COLUMN owner_id uuid REFERENCES investors(id) ON DELETE RESTRICT,
 ADD CHECK ((truck_id IS NULL AND owner_id IS NULL) OR (truck_id IS NOT NULL AND owner_id IS NOT NULL AND driver_id IS NULL));
CREATE UNIQUE INDEX investor_truck_escrow_idx ON driver_escrows(truck_id,owner_id) WHERE truck_id IS NOT NULL;
ALTER TABLE driver_charge_schedules ALTER COLUMN driver_id DROP NOT NULL,
 ADD COLUMN truck_id uuid REFERENCES trucks(id) ON DELETE RESTRICT,
 ADD COLUMN owner_id uuid REFERENCES investors(id) ON DELETE RESTRICT,
 ADD CHECK ((driver_id IS NOT NULL AND truck_id IS NULL AND owner_id IS NULL) OR (driver_id IS NULL AND truck_id IS NOT NULL AND owner_id IS NOT NULL AND kind='installment'));
CREATE INDEX investor_truck_installments_idx ON driver_charge_schedules(truck_id,owner_id);

-- A driver-linked investor may own several trucks. Exclude the truck they
-- operate, while permitting trucks staffed by other drivers. Unstaffed linked
-- owners wait for an operating assignment instead of receiving a second escrow.
CREATE FUNCTION investor_truck_account_eligible(truck uuid, owner uuid, first_day date, until_day date)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM investors i WHERE i.id=owner AND NOT i.is_company
 AND NOT EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=truck AND a.driver_id=i.driver_id
 AND a.assigned_at<(until_day::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>(first_day::timestamp AT TIME ZONE 'America/New_York')))
 AND (i.driver_id IS NULL OR EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=truck AND a.driver_id<>i.driver_id
 AND a.assigned_at<(until_day::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>(first_day::timestamp AT TIME ZONE 'America/New_York')))))
$$;

-- Agreements enroll truck accounts using the shared onboarding default. Existing
-- trucks start this week, without retroactively changing historical statements.
CREATE FUNCTION ensure_investor_truck_escrow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO driver_escrows(truck_id,owner_id,driver_name,start_date,amount)
 SELECT NEW.truck_id,NEW.owner_id,i.full_name||' - Truck '||t.unit_number,
 greatest(NEW.week_start,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date),s.default_amount
 FROM trucks t JOIN investors i ON i.id=NEW.owner_id CROSS JOIN driver_escrow_settings s
 WHERE t.id=NEW.truck_id AND investor_truck_account_eligible(t.id,i.id,(now() AT TIME ZONE 'America/New_York')::date,(now() AT TIME ZONE 'America/New_York')::date+1)
 ON CONFLICT(truck_id,owner_id) WHERE truck_id IS NOT NULL DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER investor_truck_escrow_setup AFTER INSERT OR UPDATE ON truck_settlement_terms
 FOR EACH ROW EXECUTE FUNCTION ensure_investor_truck_escrow();
INSERT INTO driver_escrows(truck_id,owner_id,driver_name,start_date,amount)
 SELECT t.id,i.id,i.full_name||' - Truck '||t.unit_number,
 date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,s.default_amount
 FROM trucks t JOIN investors i ON i.id=t.owner_id CROSS JOIN driver_escrow_settings s
 WHERE investor_truck_account_eligible(t.id,i.id,(now() AT TIME ZONE 'America/New_York')::date,(now() AT TIME ZONE 'America/New_York')::date+1)
 ON CONFLICT DO NOTHING;


CREATE FUNCTION enroll_investor_truck_escrow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO driver_escrows(truck_id,owner_id,driver_name,start_date,amount)
 SELECT NEW.id,NEW.owner_id,i.full_name||' - Truck '||NEW.unit_number,
 date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,s.default_amount
 FROM investors i CROSS JOIN driver_escrow_settings s WHERE i.id=NEW.owner_id AND investor_truck_account_eligible(NEW.id,i.id,(now() AT TIME ZONE 'America/New_York')::date,(now() AT TIME ZONE 'America/New_York')::date+1)
 ON CONFLICT(truck_id,owner_id) WHERE truck_id IS NOT NULL DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER investor_truck_escrow_onboarding AFTER INSERT OR UPDATE OF owner_id ON trucks
 FOR EACH ROW EXECUTE FUNCTION enroll_investor_truck_escrow();


-- Assignment handoffs can turn an owner's own truck into an investor truck.
CREATE FUNCTION enroll_assigned_investor_truck_escrow() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE truck uuid;
BEGIN
 truck:=CASE WHEN TG_OP='DELETE' THEN OLD.truck_id ELSE NEW.truck_id END;
 INSERT INTO driver_escrows(truck_id,owner_id,driver_name,start_date,amount)
 SELECT t.id,t.owner_id,i.full_name||' - Truck '||t.unit_number,(now() AT TIME ZONE 'America/New_York')::date,s.default_amount
 FROM trucks t JOIN investors i ON i.id=t.owner_id CROSS JOIN driver_escrow_settings s
 WHERE t.id=truck AND investor_truck_account_eligible(t.id,i.id,(now() AT TIME ZONE 'America/New_York')::date,(now() AT TIME ZONE 'America/New_York')::date+1)
 ON CONFLICT(truck_id,owner_id) WHERE truck_id IS NOT NULL DO NOTHING;
 RETURN NULL;
END $$;
CREATE TRIGGER investor_truck_escrow_assignment AFTER INSERT OR UPDATE OR DELETE ON truck_driver_assignments
 FOR EACH ROW EXECUTE FUNCTION enroll_assigned_investor_truck_escrow();

CREATE FUNCTION guard_investor_account() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_value jsonb; old_value jsonb; truck uuid; owner uuid; from_week date;
BEGIN
 row_value:=CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
 old_value:=CASE WHEN TG_OP='UPDATE' THEN to_jsonb(OLD) ELSE row_value END;
 IF TG_TABLE_NAME='driver_escrows' THEN
  truck:=(row_value->>'truck_id')::uuid; owner:=(row_value->>'owner_id')::uuid;
  IF TG_OP='UPDATE' AND (row_value-'balance_version'-'updated_at')=(old_value-'balance_version'-'updated_at') THEN RETURN NEW; END IF;
  from_week:=date_trunc('week',(row_value->>'start_date')::date)::date;
 ELSIF TG_TABLE_NAME='driver_charge_schedules' THEN
  truck:=(row_value->>'truck_id')::uuid; owner:=(row_value->>'owner_id')::uuid;
  IF TG_OP='UPDATE' AND (row_value-'version')=(old_value-'version') THEN RETURN NEW; END IF;
  from_week:=(row_value->>'start_week')::date;
 ELSIF TG_TABLE_NAME IN ('driver_escrow_payments','driver_escrow_releases') THEN
  SELECT truck_id,owner_id INTO truck,owner FROM driver_escrows WHERE id=(row_value->>'escrow_id')::uuid;
  from_week:=least((row_value->>'week_start')::date,(old_value->>'week_start')::date);
 ELSE
  SELECT truck_id,owner_id INTO truck,owner FROM driver_charge_schedules WHERE id=(row_value->>'schedule_id')::uuid;
  from_week:=least((row_value->>'week_start')::date,(old_value->>'week_start')::date);
 END IF;
 IF truck IS NOT NULL THEN
  IF TG_TABLE_NAME IN ('driver_escrows','driver_charge_schedules') AND TG_OP='UPDATE' AND
    (row_value->>'truck_id' IS DISTINCT FROM old_value->>'truck_id' OR row_value->>'owner_id' IS DISTINCT FROM old_value->>'owner_id') THEN
   RAISE EXCEPTION 'Investor account ownership cannot change' USING ERRCODE='23514';
  END IF;
  IF EXISTS(SELECT 1 FROM investor_pay_weeks WHERE truck_id=truck AND owner_id=owner AND week_start>=from_week AND finalized) THEN
   RAISE EXCEPTION 'Reopen affected investor statements before changing this account' USING ERRCODE='23514';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER investor_escrow_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrows FOR EACH ROW EXECUTE FUNCTION guard_investor_account();
CREATE TRIGGER investor_escrow_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_payments FOR EACH ROW EXECUTE FUNCTION guard_investor_account();
CREATE TRIGGER investor_escrow_release_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_releases FOR EACH ROW EXECUTE FUNCTION guard_investor_account();
CREATE TRIGGER investor_installment_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_schedules FOR EACH ROW EXECUTE FUNCTION guard_investor_account();
CREATE TRIGGER investor_installment_phase_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_phases FOR EACH ROW EXECUTE FUNCTION guard_investor_account();
CREATE TRIGGER investor_installment_occurrence_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION guard_investor_account();

CREATE OR REPLACE FUNCTION protect_escrow_release() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE driver uuid; truck uuid; current_week date := date_trunc('week',now() AT TIME ZONE 'America/New_York')::date;
BEGIN
 IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'Cancel escrow releases instead of deleting history' USING ERRCODE='23514';
 END IF;
 SELECT driver_id,truck_id INTO driver,truck FROM driver_escrows WHERE id=NEW.escrow_id FOR UPDATE;
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
 IF (driver IS NULL AND truck IS NULL AND NEW.correction_reason IS NULL)
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


-- Default-only accounts should not prevent deleting an unused truck. Any balance,
-- correction, payment or release history retains the restrictive foreign key.
CREATE FUNCTION remove_unused_investor_truck_escrow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM driver_escrows e WHERE e.truck_id=OLD.id AND e.opening_paid=0 AND e.balance_version=1
 AND NOT EXISTS(SELECT 1 FROM driver_escrow_payments p WHERE p.escrow_id=e.id)
 AND NOT EXISTS(SELECT 1 FROM driver_escrow_releases r WHERE r.escrow_id=e.id)
 AND NOT EXISTS(SELECT 1 FROM driver_escrow_opening_events h WHERE h.escrow_id=e.id);
 RETURN OLD;
END $$;
CREATE TRIGGER investor_truck_escrow_unused BEFORE DELETE ON trucks
 FOR EACH ROW EXECUTE FUNCTION remove_unused_investor_truck_escrow();

COMMIT;
