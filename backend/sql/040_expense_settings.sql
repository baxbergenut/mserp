BEGIN;

CREATE TABLE expense_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('category','name','payment_method','payer')),
    category_id UUID REFERENCES expense_settings(id) ON DELETE RESTRICT,
    name TEXT NOT NULL CHECK (name = btrim(name) AND length(name) BETWEEN 1 AND 100),
    active BOOLEAN NOT NULL DEFAULT true,
    version INTEGER NOT NULL DEFAULT 1,
    CHECK ((kind = 'name') = (category_id IS NOT NULL))
);
CREATE UNIQUE INDEX expense_settings_unique_name ON expense_settings
    (kind, coalesce(category_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));

INSERT INTO expense_settings(kind,name) VALUES
    ('category','Maintenance'),('category','Penalties'),('category','Other'),
    ('category','Safety'),('category','HR'),('category','Administrative');
INSERT INTO expense_settings(kind,category_id,name)
SELECT 'name',c.id,n.name FROM (VALUES
    ('Maintenance','Tire replacement'),('Maintenance','Electrical repair'),('Maintenance','Oil change'),
    ('Penalties','Parking violation'),('Penalties','Speeding ticket'),('Penalties','Late delivery penalty'),
    ('Safety','Inspection'),('Safety','Permit'),('Safety','Drug test'),
    ('HR','Recruiting'),('HR','Driver lodging'),
    ('Administrative','Software subscription'),('Administrative','Office supplies'),('Administrative','Bank fee')
) n(category,name) JOIN expense_settings c ON c.kind='category' AND c.name=n.category;
INSERT INTO expense_settings(kind,name)
SELECT 'payment_method',name FROM (SELECT unnest(ARRAY['Cash','ACH','Check','Card']) AS name
    UNION SELECT btrim(payment_type) FROM expenses WHERE nullif(btrim(payment_type),'') IS NOT NULL) v
WHERE length(name)<=100 ON CONFLICT DO NOTHING;
INSERT INTO expense_settings(kind,name)
SELECT 'payer',btrim(paid_by) FROM expenses WHERE length(btrim(paid_by)) BETWEEN 1 AND 100
ON CONFLICT DO NOTHING;

CREATE FUNCTION validate_expense_setting() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.kind<>OLD.kind THEN
        RAISE EXCEPTION 'Setting kind cannot change' USING ERRCODE='23514';
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
CREATE TRIGGER expense_setting_guard BEFORE INSERT OR UPDATE ON expense_settings
    FOR EACH ROW EXECUTE FUNCTION validate_expense_setting();

ALTER TABLE expenses DROP CONSTRAINT expenses_category_check;
ALTER TABLE expenses ADD CONSTRAINT expenses_category_check CHECK (length(btrim(category)) BETWEEN 1 AND 100);
CREATE FUNCTION validate_expense_category() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Existing records keep their original category even after a rename/archive.
    IF TG_OP='UPDATE' AND NEW.category=OLD.category THEN RETURN NEW; END IF;
    PERFORM 1 FROM expense_settings WHERE kind='category' AND name=NEW.category AND active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Select an active expense category' USING ERRCODE='23514', CONSTRAINT='expense_active_category';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER expense_category_guard BEFORE INSERT OR UPDATE OF category ON expenses
    FOR EACH ROW EXECUTE FUNCTION validate_expense_category();

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mserp_app') THEN
        ALTER TABLE expense_settings OWNER TO mserp_app;
    END IF;
END $$;
COMMIT;
