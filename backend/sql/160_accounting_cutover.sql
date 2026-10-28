BEGIN;

-- Installing this migration does not activate a financial cutoff.
CREATE TABLE accounting_cutover (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 version integer NOT NULL DEFAULT 1,
 active boolean NOT NULL DEFAULT false,
 week_start date NOT NULL DEFAULT '2026-10-26' CHECK(extract(isodow FROM week_start)=1),
 draft jsonb NOT NULL DEFAULT '{}',
 activated_at timestamptz,
 activated_by uuid REFERENCES app_users(id),
 receipt jsonb
);
INSERT INTO accounting_cutover(id) VALUES(true);
CREATE TABLE accounting_cutover_previews (
 id uuid PRIMARY KEY, version integer NOT NULL, revision text NOT NULL,
 result jsonb NOT NULL, archives jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), created_by uuid NOT NULL REFERENCES app_users(id)
);
CREATE TABLE accounting_payroll_archives (
 kind text NOT NULL CHECK(kind IN ('driver','investor')), week_start date NOT NULL,
 report jsonb NOT NULL, PRIMARY KEY(kind,week_start)
);
CREATE TABLE accounting_legacy_sources (
 kind text NOT NULL CHECK(kind IN ('escrow','installment','expense')),
 source_id uuid NOT NULL, PRIMARY KEY(kind,source_id)
);
CREATE TABLE accounting_rate_openings (
 driver_id uuid PRIMARY KEY REFERENCES drivers(id), amount numeric(14,2) NOT NULL,
 reference text NOT NULL CHECK(length(btrim(reference))>0)
);
CREATE TABLE accounting_rate_corrections (
 id uuid PRIMARY KEY, driver_id uuid NOT NULL REFERENCES drivers(id),
 amount numeric(14,2) NOT NULL, reference text NOT NULL CHECK(length(btrim(reference)) BETWEEN 1 AND 2000),
 actor_id uuid NOT NULL REFERENCES app_users(id), created_at timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION accounting_cutover_date() RETURNS date LANGUAGE sql STABLE AS $$
 SELECT coalesce(nullif(current_setting('mserp.cutover_preview',true),'')::date,
 (SELECT week_start FROM accounting_cutover WHERE id AND active))
$$;
CREATE FUNCTION accounting_live_date(d date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT accounting_cutover_date() IS NULL OR d>=accounting_cutover_date()
$$;
CREATE FUNCTION accounting_live_source(k text,i uuid,d date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT accounting_cutover_date() IS NULL OR (coalesce(d>=accounting_cutover_date(),false)
 AND NOT EXISTS(SELECT 1 FROM accounting_legacy_sources WHERE kind=k AND source_id=i))
$$;

CREATE FUNCTION protect_accounting_cutover() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.active THEN RAISE EXCEPTION 'An activated accounting cutover is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'The accounting cutover cannot be deleted' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER accounting_cutover_guard BEFORE UPDATE OR DELETE ON accounting_cutover FOR EACH ROW EXECUTE FUNCTION protect_accounting_cutover();
CREATE FUNCTION protect_accounting_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Accounting receipts are append-only' USING ERRCODE='23514'; END $$;
CREATE TRIGGER accounting_preview_immutable BEFORE UPDATE OR DELETE ON accounting_cutover_previews FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();
CREATE TRIGGER accounting_archive_immutable BEFORE UPDATE OR DELETE ON accounting_payroll_archives FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();

-- Guard both old and new dates, including moving a source across the boundary.
CREATE FUNCTION protect_accounting_history() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE before_row jsonb; after_row jsonb; c date; field text;
BEGIN
 SELECT week_start INTO c FROM accounting_cutover WHERE id AND active;
 IF c IS NULL THEN IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF; END IF;
 IF TG_OP<>'INSERT' THEN before_row:=to_jsonb(OLD); END IF;
 IF TG_OP<>'DELETE' THEN after_row:=to_jsonb(NEW); END IF;
 FOREACH field IN ARRAY TG_ARGV LOOP
 IF (before_row IS NOT NULL AND coalesce((before_row->>field)::date,'-infinity'::date)<c)
 OR (after_row IS NOT NULL AND coalesce((after_row->>field)::date,'-infinity'::date)<c) THEN
  RAISE EXCEPTION 'Historical accounting is managed in Sheets; use a referenced current-week adjustment' USING ERRCODE='23514';
 END IF;
 END LOOP;
 IF TG_TABLE_NAME='expenses' AND EXISTS(SELECT 1 FROM accounting_legacy_sources WHERE kind='expense' AND source_id=coalesce((before_row->>'id')::uuid,(after_row->>'id')::uuid)) THEN
  RAISE EXCEPTION 'This expense is managed in Sheets' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER accounting_transfer_destination_guard BEFORE INSERT OR UPDATE OR DELETE ON gross_board_transfers FOR EACH ROW EXECUTE FUNCTION protect_accounting_history('dispatch_date','settlement_date');
DO $$ DECLARE t text; col text; BEGIN
 FOR t,col IN SELECT * FROM (VALUES
 ('gross_board_entries','service_date'),('gross_board_extra_entries','service_date'),('gross_board_transfers','source_date'),
 ('driver_pay_weeks','week_start'),('payroll_settlements','week_start'),('investor_pay_weeks','week_start'),
 ('driver_pay_cost_collections','week_start'),('pay_source_collections','week_start'),
 ('expense_payments','week_start'),('expense_reimbursement_payments','week_start'),
 ('driver_escrow_payments','week_start'),('driver_escrow_releases','week_start'),
 ('driver_charge_occurrences','week_start'),('expenses','expense_date')) v(t,col)
 LOOP EXECUTE format('CREATE TRIGGER accounting_history_guard BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION protect_accounting_history(%L)',t,col); END LOOP;
END $$;

CREATE TABLE expense_external_settlements (
 id uuid PRIMARY KEY, expense_id uuid NOT NULL REFERENCES expenses(id),
 amount numeric(14,2) NOT NULL CHECK(amount>0), settled_on date NOT NULL,
 reference text NOT NULL CHECK(length(btrim(reference)) BETWEEN 1 AND 2000),
 reverses_id uuid UNIQUE REFERENCES expense_external_settlements(id),
 actor_id uuid NOT NULL REFERENCES app_users(id), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX expense_external_source ON expense_external_settlements(expense_id);
CREATE FUNCTION expense_external_total(i uuid) RETURNS numeric LANGUAGE sql STABLE AS $$
 SELECT coalesce(sum(CASE WHEN reverses_id IS NULL THEN amount ELSE -amount END),0)
 FROM expense_external_settlements WHERE expense_id=i
$$;
CREATE TRIGGER expense_external_immutable BEFORE UPDATE OR DELETE ON expense_external_settlements FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();

CREATE FUNCTION protect_external_settlement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; original expense_external_settlements%ROWTYPE; used numeric; total numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 SELECT * INTO e FROM expenses WHERE id=NEW.expense_id FOR UPDATE;
 IF NOT accounting_live_source('expense',e.id,e.expense_date) OR e.driver_settled THEN
  RAISE EXCEPTION 'This expense is managed outside active Clustie payroll' USING ERRCODE='23514'; END IF;
 IF e.amount IS NULL OR (CASE WHEN lower(btrim(e.payment_type))='reimburse' THEN e.driver_id ELSE coalesce(e.charge_driver_id,e.owner_id) END) IS NULL THEN
  RAISE EXCEPTION 'Review the amount and responsible person before recording a settlement' USING ERRCODE='23514'; END IF;
 IF NEW.settled_on>(now() AT TIME ZONE 'America/New_York')::date OR NOT accounting_live_date(NEW.settled_on) THEN
  RAISE EXCEPTION 'Use an actual settlement date within active Clustie accounting' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized AND
 (s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',e.id::text))))
 OR s.report @> jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','expense-reimburse:'||e.id::text)))))
 OR EXISTS(SELECT 1 FROM investor_pay_weeks s WHERE s.finalized AND s.report @> jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',e.id::text))))) THEN
  RAISE EXCEPTION 'Reopen affected finalized statements before recording or reversing an external settlement' USING ERRCODE='23514'; END IF;
 IF NEW.reverses_id IS NOT NULL THEN
  SELECT * INTO original FROM expense_external_settlements WHERE id=NEW.reverses_id;
  IF NOT FOUND OR original.reverses_id IS NOT NULL OR (original.expense_id,original.amount) IS DISTINCT FROM (NEW.expense_id,NEW.amount) THEN
   RAISE EXCEPTION 'Reverse the exact original settlement' USING ERRCODE='23514'; END IF;
 ELSE
  SELECT coalesce(sum(amount),0) INTO used FROM (
   SELECT amount FROM expense_payments WHERE expense_id=e.id UNION ALL
   SELECT amount FROM expense_reimbursement_payments WHERE expense_id=e.id) p;
  total:=e.amount;
  IF e.base_amount IS NULL AND e.payment_fee=0 AND upper(btrim(e.payment_type))='EFS' AND lower(btrim(e.covered_by)) IN ('driver','truck owner') AND e.amount>0
  AND NOT EXISTS(SELECT 1 FROM expense_settings WHERE id=e.category_id AND driver_only) THEN total:=total+ceil(total/500)*3; END IF;
  IF NEW.amount>total-used-expense_external_total(e.id) THEN RAISE EXCEPTION 'Settlement exceeds the unreserved balance' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_external_guard BEFORE INSERT ON expense_external_settlements FOR EACH ROW EXECUTE FUNCTION protect_external_settlement();
CREATE FUNCTION touch_external_expense() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN UPDATE expenses SET updated_at=now() WHERE id=NEW.expense_id; RETURN NULL; END $$;
CREATE TRIGGER expense_external_touch AFTER INSERT ON expense_external_settlements FOR EACH ROW EXECUTE FUNCTION touch_external_expense();

CREATE FUNCTION protect_external_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM expense_external_settlements WHERE expense_id=OLD.id) AND
 (NEW.driver_id,NEW.charge_driver_id,NEW.owner_id,NEW.truck_id,NEW.expense_date,NEW.amount,NEW.base_amount,NEW.payment_fee,NEW.payment_type,NEW.covered_by,NEW.category_id,NEW.driver_settled)
 IS DISTINCT FROM (OLD.driver_id,OLD.charge_driver_id,OLD.owner_id,OLD.truck_id,OLD.expense_date,OLD.amount,OLD.base_amount,OLD.payment_fee,OLD.payment_type,OLD.covered_by,OLD.category_id,OLD.driver_settled) THEN
 RAISE EXCEPTION 'An externally settled expense retains its identity and principal' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER expense_external_principal BEFORE UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION protect_external_principal();

CREATE FUNCTION protect_active_expense_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e expenses%ROWTYPE; used numeric; total numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(736281940);
 SELECT * INTO e FROM expenses WHERE id=NEW.expense_id FOR UPDATE;
 IF NOT accounting_live_source('expense',e.id,e.expense_date) THEN RAISE EXCEPTION 'This expense is managed in Sheets' USING ERRCODE='23514'; END IF;
 SELECT coalesce(sum(amount),0) INTO used FROM (
 SELECT amount FROM expense_payments WHERE expense_id=e.id AND (TG_TABLE_NAME<>'expense_payments' OR week_start<>NEW.week_start)
 UNION ALL SELECT amount FROM expense_reimbursement_payments WHERE expense_id=e.id AND (TG_TABLE_NAME<>'expense_reimbursement_payments' OR week_start<>NEW.week_start)) p;
 total:=e.amount;
 IF e.base_amount IS NULL AND e.payment_fee=0 AND upper(btrim(e.payment_type))='EFS' AND lower(btrim(e.covered_by)) IN ('driver','truck owner') AND e.amount>0 AND NOT e.driver_settled
 AND NOT EXISTS(SELECT 1 FROM expense_settings WHERE id=e.category_id AND driver_only) THEN total:=total+ceil(total/500)*3; END IF;
 IF NEW.amount>total-used-expense_external_total(e.id) THEN RAISE EXCEPTION 'Payment exceeds the unreserved balance' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER active_expense_payment BEFORE INSERT OR UPDATE ON expense_payments FOR EACH ROW EXECUTE FUNCTION protect_active_expense_payment();
CREATE TRIGGER active_reimburse_payment BEFORE INSERT OR UPDATE ON expense_reimbursement_payments FOR EACH ROW EXECUTE FUNCTION protect_active_expense_payment();

CREATE FUNCTION protect_sheets_managed_plan() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb; k text; source uuid; start_day date;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM accounting_cutover WHERE active) THEN IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF; END IF;
 row_data:=CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
 k:=TG_ARGV[0];source:=(row_data->>TG_ARGV[1])::uuid;
 IF k='escrow' THEN SELECT start_date INTO start_day FROM driver_escrows WHERE id=source;
 ELSE SELECT start_week INTO start_day FROM driver_charge_schedules WHERE id=source AND kind='installment';
  IF NOT FOUND THEN IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF; END IF;
 END IF;
 IF NOT coalesce(accounting_live_source(k,source,start_day),false) THEN
  RAISE EXCEPTION 'This existing plan is managed in Sheets; use a referenced payroll adjustment' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER sheets_escrow_guard BEFORE UPDATE OR DELETE ON driver_escrows FOR EACH ROW EXECUTE FUNCTION protect_sheets_managed_plan('escrow','id');
CREATE TRIGGER sheets_escrow_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_payments FOR EACH ROW EXECUTE FUNCTION protect_sheets_managed_plan('escrow','escrow_id');
CREATE TRIGGER sheets_escrow_release_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_escrow_releases FOR EACH ROW EXECUTE FUNCTION protect_sheets_managed_plan('escrow','escrow_id');
CREATE TRIGGER sheets_installment_guard BEFORE UPDATE OR DELETE ON driver_charge_schedules FOR EACH ROW EXECUTE FUNCTION protect_sheets_managed_plan('installment','id');
CREATE TRIGGER sheets_installment_phase_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_phases FOR EACH ROW EXECUTE FUNCTION protect_sheets_managed_plan('installment','schedule_id');
CREATE TRIGGER sheets_installment_occurrence_guard BEFORE INSERT OR UPDATE OR DELETE ON driver_charge_occurrences FOR EACH ROW EXECUTE FUNCTION protect_sheets_managed_plan('installment','schedule_id');
CREATE TRIGGER accounting_rate_opening_immutable BEFORE UPDATE OR DELETE ON accounting_rate_openings FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();
CREATE TRIGGER accounting_rate_correction_immutable BEFORE UPDATE OR DELETE ON accounting_rate_corrections FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();
CREATE TRIGGER accounting_legacy_source_immutable BEFORE UPDATE OR DELETE ON accounting_legacy_sources FOR EACH ROW EXECUTE FUNCTION protect_accounting_receipt();

CREATE FUNCTION protect_sheets_adjustment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE adjustments jsonb;
BEGIN
 adjustments:=CASE WHEN TG_TABLE_NAME='driver_pay_weeks' THEN to_jsonb(NEW)->'adjustments' ELSE to_jsonb(NEW)->'edits'->'adjustments' END;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(adjustments,'[]')) a WHERE a->>'legacySheets'='true' AND coalesce(btrim(a->>'note'),'')='') THEN
  RAISE EXCEPTION 'Sheets adjustments require a Sheets reference' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER sheets_adjustment_guard BEFORE INSERT OR UPDATE ON driver_pay_weeks FOR EACH ROW EXECUTE FUNCTION protect_sheets_adjustment();
CREATE TRIGGER sheets_adjustment_guard BEFORE INSERT OR UPDATE ON investor_pay_weeks FOR EACH ROW EXECUTE FUNCTION protect_sheets_adjustment();

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['accounting_cutover','accounting_cutover_previews','accounting_payroll_archives','accounting_legacy_sources','accounting_rate_openings','accounting_rate_corrections','expense_external_settlements'] LOOP
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN EXECUTE format('ALTER TABLE %I OWNER TO mserp_app',t); END IF;
  INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
  EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 END LOOP;
 -- Later feature migrations introduced tables after the original revision
 -- registry. Enroll them so unrelated database transactions do not invalidate
 -- every payroll read. Future untracked tables still trigger safe fallback.
 FOR t IN SELECT tablename FROM pg_tables p WHERE schemaname=current_schema()
 AND tablename NOT IN ('payroll_cache_revisions','auth_sessions','user_login_activity','schema_migrations')
 AND NOT EXISTS(SELECT 1 FROM payroll_cache_revisions r WHERE r.table_name=p.tablename) ORDER BY tablename LOOP
  INSERT INTO payroll_cache_revisions(table_name) VALUES(t);
  EXECUTE format('CREATE TRIGGER payroll_cache_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_update AFTER UPDATE ON %I REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
  EXECUTE format('CREATE TRIGGER payroll_cache_truncate AFTER TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache()',t);
 END LOOP;
END $$;
COMMIT;
