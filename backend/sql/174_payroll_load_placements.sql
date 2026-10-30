BEGIN;

-- Payroll-only recognition. Dispatch plans and provider assignments are untouched.
-- One decision per imported allocation prevents an extra row from paying it twice.
CREATE TABLE payroll_load_placements (
 load_record_id integer NOT NULL REFERENCES loads(id),
 load_trip_id integer NOT NULL DEFAULT 0 CHECK(load_trip_id>=0),
 driver_id uuid NOT NULL REFERENCES drivers(id),
 truck_id uuid REFERENCES trucks(id),
 source_date date NOT NULL,
 source_slot integer NOT NULL CHECK(source_slot BETWEEN 0 AND 99),
 week_start date NOT NULL CHECK(extract(isodow FROM week_start)=1),
 slot integer NOT NULL CHECK(slot BETWEEN 0 AND 99),
 included boolean NOT NULL,
 driver_gross numeric(14,2),
 original_rate numeric(14,2),
 miles numeric(14,2),
 version integer NOT NULL DEFAULT 1,
 PRIMARY KEY(load_record_id,load_trip_id)
);
CREATE UNIQUE INDEX payroll_placement_slot ON payroll_load_placements(driver_id,week_start,slot) WHERE included;
CREATE TABLE payroll_load_placement_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id text NOT NULL,
 action text NOT NULL,
 before_value jsonb,
 after_value jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER protect_payroll_placement_history BEFORE UPDATE OR DELETE ON payroll_load_placement_history
 FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
CREATE FUNCTION guard_payroll_load_placement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p payroll_load_placements; frozen boolean;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 IF TG_OP='DELETE' THEN p:=OLD; ELSE p:=NEW; END IF;
 SELECT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND s.week_start=p.week_start
 AND s.driver_id IN (p.driver_id,payroll_driver_at(p.driver_id,p.week_start)))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.finalized AND w.week_start=p.week_start
 AND (w.truck_id=p.truck_id OR EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(w.report->'loads','[]')) l WHERE l->>'sourceDriverId'=p.driver_id::text))) INTO frozen;
 IF TG_OP='UPDATE' AND OLD.included THEN
 frozen:=frozen OR EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND s.week_start=OLD.week_start
 AND s.driver_id IN (OLD.driver_id,payroll_driver_at(OLD.driver_id,OLD.week_start)))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.finalized AND w.week_start=OLD.week_start AND w.truck_id=OLD.truck_id);
 END IF;
 IF frozen OR EXISTS(SELECT 1 FROM (
 SELECT report FROM payroll_settlements WHERE finalized UNION ALL SELECT report FROM investor_pay_weeks WHERE finalized) s
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.report->'loads','[]')) l
 WHERE ((l->>'loadRecordId')::integer=p.load_record_id OR (l->>'loadRecordId' IS NULL AND lower(btrim(l->>'loadNumber'))=(SELECT lower(btrim(load_id)) FROM loads WHERE id=p.load_record_id)))
 AND (coalesce((l->>'loadTripId')::integer,0)=p.load_trip_id OR l->>'loadTripId' IS NULL OR p.load_trip_id=0)) THEN
 RAISE EXCEPTION 'Reopen affected driver and investor statements before changing this payroll load' USING ERRCODE='23514';
 END IF;
 RETURN p;
END $$;
CREATE TRIGGER guard_payroll_placement BEFORE INSERT OR UPDATE OR DELETE ON payroll_load_placements
 FOR EACH ROW EXECUTE FUNCTION guard_payroll_load_placement();
DO $$ DECLARE tbl text; BEGIN
 FOREACH tbl IN ARRAY ARRAY['payroll_load_placements','payroll_load_placement_history'] LOOP
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',tbl);
 END IF;
 IF to_regclass('payroll_cache_revisions') IS NOT NULL THEN
 INSERT INTO payroll_cache_revisions(table_name) VALUES(tbl) ON CONFLICT DO NOTHING;
 EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',tbl);
 EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',tbl);
 EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',tbl);
 END IF;
 END LOOP;
END $$;
COMMIT;
