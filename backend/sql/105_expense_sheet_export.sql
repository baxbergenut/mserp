BEGIN;

-- Explicit operator activation is required. Deploying this migration sends nothing.
CREATE TABLE expense_sheet_export_config (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 enabled boolean NOT NULL DEFAULT false,
 spreadsheet_id text NOT NULL DEFAULT '1VKSL8fyBRjwOWAUhHlHUrJtn2HOa53tpuTm95TNqll4',
 maintenance_sheet_id integer NOT NULL DEFAULT 377431143,
 hr_sheet_id integer NOT NULL DEFAULT 1713619712,
 safety_sheet_id integer NOT NULL DEFAULT 752928518,
 paid_before date NOT NULL DEFAULT '2026-09-28',
 hr_category_id uuid NOT NULL REFERENCES expense_settings(id),
 safety_category_id uuid NOT NULL REFERENCES expense_settings(id),
 penalties_category_id uuid NOT NULL REFERENCES expense_settings(id),
 other_category_id uuid NOT NULL REFERENCES expense_settings(id)
);
INSERT INTO expense_sheet_export_config(hr_category_id,other_category_id,safety_category_id,penalties_category_id)
SELECT h.id,o.id,s.id,p.id FROM expense_settings h CROSS JOIN expense_settings o CROSS JOIN expense_settings s CROSS JOIN expense_settings p
WHERE h.kind='category' AND lower(h.name)='hr' AND o.kind='category' AND lower(o.name)='other'
 AND s.kind='category' AND lower(s.name)='safety' AND p.kind='category' AND lower(p.name)='penalties';

-- Customer-confirmed transition cutoff: historical native entries are already paid.
-- This sets settlement only; verification/approval checkboxes retain their meaning.
CREATE FUNCTION settle_expense_sheet_history() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c expense_sheet_export_config;
BEGIN
 SELECT * INTO c FROM expense_sheet_export_config WHERE singleton;
 IF FOUND AND c.enabled AND NEW.source_spreadsheet_id IS NULL AND NEW.system_kind IS NULL
 AND NEW.expense_date<c.paid_before AND (NEW.category_id IN(c.hr_category_id,c.safety_category_id,c.penalties_category_id,c.other_category_id)
 OR EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=NEW.category_id)) THEN
  NEW.driver_settled:=true;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_sheet_history_settled BEFORE INSERT ON expenses
 FOR EACH ROW EXECUTE FUNCTION settle_expense_sheet_history();

-- No foreign key to the source: deletion must not discard an already queued backup.
CREATE TABLE expense_sheet_exports (
 expense_id uuid PRIMARY KEY,
 spreadsheet_id text NOT NULL,
 sheet_id integer NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 last_attempt_at timestamptz,
 delivered_at timestamptz
);
CREATE INDEX expense_sheet_exports_pending ON expense_sheet_exports(created_at,expense_id) WHERE delivered_at IS NULL;

CREATE FUNCTION capture_expense_sheet_export() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c expense_sheet_export_config; e expenses; m jsonb; target integer;
BEGIN
 SELECT * INTO c FROM expense_sheet_export_config WHERE singleton;
 IF NOT FOUND OR NOT c.enabled THEN RETURN NEW; END IF;
 SELECT * INTO e FROM expenses WHERE id=NEW.id;
 IF NOT FOUND OR e.source_spreadsheet_id IS NOT NULL OR e.system_kind IS NOT NULL THEN RETURN NEW; END IF;
 IF e.category_id=c.hr_category_id THEN target:=c.hr_sheet_id;
 ELSIF e.category_id IN(c.safety_category_id,c.penalties_category_id) THEN target:=c.safety_sheet_id;
 ELSIF e.category_id=c.other_category_id OR EXISTS(SELECT 1 FROM maintenance_configuration WHERE category_id=e.category_id) THEN target:=c.maintenance_sheet_id;
 ELSE RETURN NEW;
 END IF;
 -- Deferred capture sees service details and cost allocation committed with the invoice.
 SELECT to_jsonb(r) INTO m FROM maintenance_records r WHERE expense_id=e.id;
 INSERT INTO expense_sheet_exports(expense_id,spreadsheet_id,sheet_id,payload)
 VALUES(e.id,c.spreadsheet_id,target,jsonb_build_object('expense',to_jsonb(e),'maintenance',m))
 ON CONFLICT(expense_id) DO NOTHING;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER expense_sheet_export_capture AFTER INSERT ON expenses
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION capture_expense_sheet_export();

CREATE FUNCTION protect_expense_sheet_export() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.expense_id IS DISTINCT FROM OLD.expense_id OR NEW.spreadsheet_id IS DISTINCT FROM OLD.spreadsheet_id
 OR NEW.sheet_id IS DISTINCT FROM OLD.sheet_id OR NEW.payload IS DISTINCT FROM OLD.payload
 OR NEW.created_at IS DISTINCT FROM OLD.created_at
 OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at) THEN
  RAISE EXCEPTION 'Expense sheet export snapshots and delivery receipts are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER expense_sheet_export_immutable BEFORE UPDATE ON expense_sheet_exports
 FOR EACH ROW EXECUTE FUNCTION protect_expense_sheet_export();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
  ALTER TABLE expense_sheet_export_config OWNER TO mserp_app;
  ALTER TABLE expense_sheet_exports OWNER TO mserp_app;
 END IF;
END $$;
COMMIT;
