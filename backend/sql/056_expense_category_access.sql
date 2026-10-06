BEGIN;

-- Give every historical expense a durable category identity. Categories that
-- predate the configurable catalog remain available for history and access
-- assignment, but stay archived for new entry.
INSERT INTO expense_settings(kind,name,active)
SELECT 'category', min(btrim(e.category)), false
FROM expenses e
WHERE NOT EXISTS (
    SELECT 1 FROM expense_settings s
    WHERE s.kind='category' AND lower(s.name)=lower(btrim(e.category))
)
GROUP BY lower(btrim(e.category));

ALTER TABLE expenses ADD COLUMN category_id UUID REFERENCES expense_settings(id);
UPDATE expenses e SET category_id=s.id
FROM expense_settings s
WHERE s.kind='category' AND lower(s.name)=lower(btrim(e.category));
ALTER TABLE expenses ALTER COLUMN category_id SET NOT NULL;
CREATE INDEX expenses_category_id_date_idx ON expenses(category_id,expense_date DESC);

ALTER TABLE expenses ADD COLUMN created_by UUID REFERENCES app_users(id) ON DELETE SET NULL;
ALTER TABLE expenses ADD COLUMN created_by_name TEXT;

CREATE TABLE role_expense_category_access (
    role_id UUID NOT NULL REFERENCES app_roles(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES expense_settings(id) ON DELETE CASCADE,
    can_view BOOLEAN NOT NULL DEFAULT false,
    can_create BOOLEAN NOT NULL DEFAULT false,
    can_edit BOOLEAN NOT NULL DEFAULT false,
    can_delete BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY(role_id,category_id),
    CHECK (can_view OR can_create OR can_edit OR can_delete)
);

-- Preserve existing broad access. Settings management was previously bundled
-- into expenses.write, so roles with that permission retain it explicitly.
INSERT INTO role_expense_category_access(role_id,category_id,can_view,can_create,can_edit,can_delete)
SELECT r.id,c.id,
       'expenses.read'=ANY(r.permissions),
       'expenses.write'=ANY(r.permissions),
       'expenses.write'=ANY(r.permissions),
       'expenses.write'=ANY(r.permissions)
FROM app_roles r
CROSS JOIN expense_settings c
WHERE NOT r.system_role AND c.kind='category'
  AND ('expenses.read'=ANY(r.permissions) OR 'expenses.write'=ANY(r.permissions));

UPDATE app_roles
SET permissions = CASE
    WHEN 'expenses.write'=ANY(permissions)
      AND NOT 'expense_settings.manage'=ANY(permissions)
      THEN array_append(array_remove(array_remove(permissions,'expenses.read'),'expenses.write'),'expense_settings.manage')
    ELSE array_remove(array_remove(permissions,'expenses.read'),'expenses.write')
END;

DROP TRIGGER expense_category_guard ON expenses;
CREATE OR REPLACE FUNCTION validate_expense_category() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_name TEXT;
BEGIN
    -- The saved name is a historical snapshot. Unrelated edits do not rewrite
    -- it after a category rename or archive.
    IF TG_OP='UPDATE' AND NEW.category_id=OLD.category_id THEN
        IF NEW.category IS DISTINCT FROM OLD.category THEN
            RAISE EXCEPTION 'Expense category text cannot change without its category id' USING ERRCODE='23514', CONSTRAINT='expense_active_category';
        END IF;
        RETURN NEW;
    END IF;
    SELECT name INTO current_name FROM expense_settings
    WHERE id=NEW.category_id AND kind='category' AND active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514', CONSTRAINT='expense_active_category';
    END IF;
    NEW.category:=current_name;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_category_guard BEFORE INSERT OR UPDATE OF category,category_id ON expenses
    FOR EACH ROW EXECUTE FUNCTION validate_expense_category();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE role_expense_category_access OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
