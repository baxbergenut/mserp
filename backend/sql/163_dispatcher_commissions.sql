BEGIN;
-- Rule revisions are immutable. Latest effective week, then revision, wins.
CREATE TABLE dispatcher_commission_rules (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 effective_week date NOT NULL CHECK (extract(isodow FROM effective_week)=1),
 rules jsonb NOT NULL CHECK (jsonb_typeof(rules)='object'),
 actor_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dispatcher_commission_effective ON dispatcher_commission_rules(effective_week DESC,id DESC);
CREATE TRIGGER protect_dispatcher_rules BEFORE UPDATE OR DELETE ON dispatcher_commission_rules
 FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
INSERT INTO dispatcher_commission_rules(effective_week,rules,actor_id) VALUES
 (date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,
 '{"homeDayCredit":1100,"maxTierPromotion":1,"tiers":[{"minGross":7500,"brackets":[{"minRPM":2.20,"percent":1},{"minRPM":2.31,"percent":1.25},{"minRPM":2.70,"percent":1.5}]},{"minGross":9000,"brackets":[{"minRPM":2.40,"percent":1},{"minRPM":2.61,"percent":1.5},{"minRPM":2.90,"percent":1.75}]},{"minGross":11000,"brackets":[{"minRPM":2.50,"percent":1.25},{"minRPM":2.71,"percent":1.75},{"minRPM":2.90,"percent":2}]}]}',
 'workbook-default');
CREATE TABLE dispatcher_pay_inputs (
 week_start date NOT NULL CHECK (extract(isodow FROM week_start)=1),
 driver_id uuid NOT NULL REFERENCES drivers(id),
 home_days integer NOT NULL CHECK(home_days BETWEEN 0 AND 7),
 note text NOT NULL DEFAULT '' CHECK(length(note)<=1000),
 version integer NOT NULL DEFAULT 1,
 PRIMARY KEY(week_start,driver_id)
);
CREATE TABLE dispatcher_pay_statements (
 week_start date NOT NULL CHECK (extract(isodow FROM week_start)=1),
 dispatcher_id text NOT NULL,
 finalized boolean NOT NULL DEFAULT true,
 report jsonb NOT NULL,
 version integer NOT NULL DEFAULT 1,
 actor_id text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(week_start,dispatcher_id)
);
CREATE TABLE dispatcher_pay_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 week_start date NOT NULL,
 actor_id text NOT NULL,
 action text NOT NULL,
 before_value jsonb,
 after_value jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER protect_dispatcher_pay_history BEFORE UPDATE OR DELETE ON dispatcher_pay_history
 FOR EACH ROW EXECUTE FUNCTION protect_driver_board_history();
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
 ALTER TABLE dispatcher_commission_rules OWNER TO mserp_app;
 ALTER SEQUENCE dispatcher_commission_rules_id_seq OWNER TO mserp_app;
 ALTER TABLE dispatcher_pay_inputs OWNER TO mserp_app;
 ALTER TABLE dispatcher_pay_statements OWNER TO mserp_app;
 ALTER TABLE dispatcher_pay_history OWNER TO mserp_app;
 ALTER SEQUENCE dispatcher_pay_history_id_seq OWNER TO mserp_app;
END IF; END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['dispatcher_commission_rules','dispatcher_pay_inputs','dispatcher_pay_statements','dispatcher_pay_history'] LOOP
  INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
  EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 END LOOP;
END $$;
COMMIT;
