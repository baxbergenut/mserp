BEGIN;

-- Tables created by earlier migrations are owned by the runtime role. PostgreSQL
-- checks their foreign keys with that owner's privileges, including while this
-- migration is run by an administrator in a private test schema.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO mserp_app', current_schema());
    END IF;
END $$;

-- Driver escrow is a normal driver-covered Safety expense. The singleton
-- setting supplies the default for new-driver setup while each created expense
-- keeps its original principal independently.
UPDATE expense_settings
SET active=true, version=version+1
WHERE kind='category' AND lower(name)='safety' AND NOT active;

INSERT INTO expense_settings(kind,name,active)
SELECT 'category','Safety',true
WHERE NOT EXISTS (
    SELECT 1 FROM expense_settings WHERE kind='category' AND lower(name)='safety'
);

INSERT INTO expense_settings(kind,category_id,name,active)
SELECT 'name',id,'Escrow payment',true
FROM expense_settings
WHERE kind='category' AND lower(name)='safety'
ON CONFLICT (kind, (coalesce(category_id, '00000000-0000-0000-0000-000000000000'::uuid)), (lower(name)))
DO UPDATE SET active=true, version=expense_settings.version+1;

CREATE TABLE driver_escrow_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    category_id UUID NOT NULL REFERENCES expense_settings(id) ON DELETE RESTRICT,
    default_amount NUMERIC(14,2) NOT NULL CHECK (default_amount > 0),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO driver_escrow_settings(singleton,category_id,default_amount)
SELECT true,id,2500
FROM expense_settings
WHERE kind='category' AND lower(name)='safety';

ALTER TABLE expenses ADD COLUMN system_kind TEXT
    CHECK (system_kind IS NULL OR system_kind='driver_escrow');
CREATE UNIQUE INDEX expenses_driver_escrow_unique
    ON expenses(driver_id) WHERE system_kind='driver_escrow';

-- Preserve a pre-existing canonical escrow instead of creating a duplicate.
WITH existing AS (
    SELECT DISTINCT ON (e.driver_id) e.id
    FROM expenses e
    JOIN driver_escrow_settings s ON s.category_id=e.category_id
    WHERE e.driver_id IS NOT NULL
      AND lower(btrim(coalesce(e.covered_by,'')))='driver'
      AND lower(btrim(coalesce(e.expense_type,'')))='escrow payment'
    ORDER BY e.driver_id,e.created_at,e.id
)
UPDATE expenses e SET system_kind='driver_escrow'
FROM existing x WHERE e.id=x.id;

INSERT INTO expenses(
    company,category_id,category,expense_date,driver_id,driver_name,amount,
    expense_type,description,covered_by,paid_by,system_kind
)
SELECT 'MS Express',s.category_id,c.name,
       coalesce(d.hire_date,(now() AT TIME ZONE 'America/New_York')::date),
       d.id,d.full_name,s.default_amount,'Escrow payment',
       'Driver safety escrow','Driver','MS Express','driver_escrow'
FROM drivers d
CROSS JOIN driver_escrow_settings s
JOIN expense_settings c ON c.id=s.category_id
WHERE NOT EXISTS (
    SELECT 1 FROM expenses e
    WHERE e.driver_id=d.id AND e.system_kind='driver_escrow'
);

CREATE OR REPLACE FUNCTION validate_expense_setting() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.kind<>OLD.kind THEN
        RAISE EXCEPTION 'Setting kind cannot change' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE'
       AND OLD.id=(SELECT category_id FROM driver_escrow_settings WHERE singleton)
       AND (NEW.name<>'Safety' OR NOT NEW.active) THEN
        RAISE EXCEPTION 'The Safety category is required for driver escrow'
            USING ERRCODE='23514', CONSTRAINT='driver_escrow_category';
    END IF;
    IF NEW.kind='name' THEN
        PERFORM 1 FROM expense_settings WHERE id=NEW.category_id AND kind='category'
            AND (active OR NOT NEW.active) FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE driver_escrow_settings OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
