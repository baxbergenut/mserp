BEGIN;

-- The operational reset was explicitly reviewed and performed separately.
-- Retiring this unused workflow must never reinterpret an activated ledger.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM accounting_cutover WHERE active)
 OR EXISTS(SELECT 1 FROM accounting_cutover_previews)
 OR EXISTS(SELECT 1 FROM accounting_payroll_archives)
 OR EXISTS(SELECT 1 FROM accounting_legacy_sources)
 OR EXISTS(SELECT 1 FROM accounting_rate_openings)
 OR EXISTS(SELECT 1 FROM accounting_rate_corrections) THEN
  RAISE EXCEPTION 'Review existing accounting cutover records before retiring this workflow';
 END IF;
END $$;

-- Retain migration-160 schema compatibility for existing payroll queries and
-- older release rollback. No screen, endpoint, draft or activation remains.
-- Constant predicates also eliminate per-source configuration lookups.
CREATE OR REPLACE FUNCTION accounting_cutover_date() RETURNS date LANGUAGE sql STABLE AS $$ SELECT NULL::date $$;
CREATE OR REPLACE FUNCTION accounting_live_date(d date) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;
CREATE OR REPLACE FUNCTION accounting_live_source(k text,i uuid,d date) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT true $$;
CREATE OR REPLACE FUNCTION protect_accounting_cutover() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Accounting cutover has been retired' USING ERRCODE='23514'; END $$;

-- Remove guards belonging exclusively to the retired cutoff. Preserve ordinary
-- finalization, balance/reservation and external-settlement receipt guards.
DO $$ DECLARE r record; BEGIN
 FOR r IN SELECT t.tgname,c.relname FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
 WHERE NOT t.tgisinternal AND c.relnamespace=current_schema()::regnamespace
 AND t.tgfoid IN ('protect_accounting_history()'::regprocedure,'protect_sheets_managed_plan()'::regprocedure)
 LOOP EXECUTE format('DROP TRIGGER %I ON %I',r.tgname,r.relname); END LOOP;
END $$;

COMMIT;
