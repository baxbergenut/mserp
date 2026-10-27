BEGIN;

-- Placement is independent of the immutable board source coordinates. No
-- delivery time is asserted by an accountant's manual recognition decision.
CREATE TABLE gross_board_transfers (
 driver_id uuid NOT NULL REFERENCES drivers(id),
 source_date date NOT NULL CHECK (extract(isodow FROM source_date) IN (6,7)),
 source_slot integer NOT NULL CHECK (source_slot BETWEEN 0 AND 99),
 dispatch_date date NOT NULL CHECK (dispatch_date=date_trunc('week',source_date)::date+7),
 dispatch_slot integer NOT NULL CHECK (dispatch_slot BETWEEN 0 AND 99),
 settlement_date date NOT NULL CHECK (settlement_date IN (source_date,dispatch_date)),
 dispatcher_id text NOT NULL,
 dispatcher_name text NOT NULL,
 truck_id uuid REFERENCES trucks(id),
 version integer NOT NULL DEFAULT 1,
 active boolean NOT NULL DEFAULT true,
 PRIMARY KEY(driver_id,source_date,source_slot)
);
CREATE UNIQUE INDEX gross_transfer_destination ON gross_board_transfers(driver_id,dispatch_date,dispatch_slot) WHERE active;
CREATE INDEX gross_transfer_source_dates ON gross_board_transfers(source_date);
CREATE INDEX gross_transfer_dispatch_dates ON gross_board_transfers(dispatch_date);
CREATE TABLE gross_board_transfer_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id text NOT NULL,
 reason text NOT NULL,
 before_value jsonb,
 after_value jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER protect_gross_transfer_history BEFORE UPDATE OR DELETE ON gross_board_transfer_history
 FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 ALTER TABLE gross_board_transfers OWNER TO mserp_app;
 ALTER TABLE gross_board_transfer_history OWNER TO mserp_app;
 ALTER SEQUENCE gross_board_transfer_history_id_seq OWNER TO mserp_app;
END IF; END $$;
CREATE FUNCTION guard_transfer_settlement_weeks() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE before_date date; after_date date;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 before_date:=NEW.source_date;
 IF TG_OP='UPDATE' AND OLD.active THEN before_date:=OLD.settlement_date; END IF;
 IF TG_OP='INSERT' THEN
 SELECT CASE WHEN active THEN settlement_date ELSE source_date END INTO before_date FROM gross_board_transfers WHERE driver_id=NEW.driver_id AND source_date=NEW.source_date AND source_slot=NEW.source_slot;
 IF NOT FOUND THEN before_date:=NEW.source_date; END IF;
 END IF;
 after_date:=CASE WHEN NEW.active THEN NEW.settlement_date ELSE NEW.source_date END;
 IF before_date IS DISTINCT FROM after_date AND (
 EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND s.week_start IN (date_trunc('week',before_date)::date,date_trunc('week',after_date)::date)
 AND (s.driver_id=NEW.driver_id OR s.driver_id=payroll_driver_at(NEW.driver_id,s.week_start)))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.finalized AND w.week_start IN (date_trunc('week',before_date)::date,date_trunc('week',after_date)::date)
 AND (w.truck_id=NEW.truck_id OR EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(w.report->'loads','[]')) l WHERE l->>'sourceDriverId'=NEW.driver_id::text)))) THEN
 RAISE EXCEPTION 'Reopen affected driver and investor statements in both weeks before moving the settlement' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_transfer_settlements BEFORE INSERT OR UPDATE ON gross_board_transfers FOR EACH ROW EXECUTE FUNCTION guard_transfer_settlement_weeks();
CREATE FUNCTION guard_transferred_board_entry() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_value jsonb; d uuid; day date; n integer;
BEGIN
 row_value := CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
 d := (row_value->>'driver_id')::uuid; day := (row_value->>'service_date')::date; n := coalesce((row_value->>'slot')::integer,0);
 IF EXISTS(SELECT 1 FROM gross_board_transfers WHERE driver_id=d AND active AND
 ((source_date=day AND source_slot=n) OR (dispatch_date=day AND dispatch_slot=n))) THEN
 RAISE EXCEPTION 'Restore this transferred load in Driver Pay or Investor Pay before editing its Gross Board entry' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_gross_transfer BEFORE INSERT OR UPDATE OR DELETE ON gross_board_entries FOR EACH ROW EXECUTE FUNCTION guard_transferred_board_entry();
CREATE TRIGGER guard_gross_transfer BEFORE INSERT OR UPDATE OR DELETE ON gross_board_extra_entries FOR EACH ROW EXECUTE FUNCTION guard_transferred_board_entry();
DO $$ DECLARE tbl text; BEGIN
 IF to_regclass('payroll_cache_revisions') IS NOT NULL THEN
 FOREACH tbl IN ARRAY ARRAY['gross_board_transfers','gross_board_transfer_history'] LOOP
 INSERT INTO payroll_cache_revisions(table_name) VALUES(tbl) ON CONFLICT DO NOTHING;
 EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',tbl);
 EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',tbl);
 END LOOP;
 END IF;
END $$;
COMMIT;
