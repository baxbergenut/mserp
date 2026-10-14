BEGIN;

-- Names are historical snapshots; catalog changes never rewrite expense costs.
ALTER TABLE expenses ADD COLUMN cost_breakdown JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE FUNCTION valid_expense_cost_breakdown(items JSONB, total NUMERIC)
RETURNS BOOLEAN LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE item JSONB; cost NUMERIC := 0;
BEGIN
    IF jsonb_typeof(items) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
    IF jsonb_array_length(items) = 0 THEN RETURN true; END IF;
    IF jsonb_array_length(items) > 100 OR total IS NULL THEN RETURN false; END IF;
    FOR item IN SELECT value FROM jsonb_array_elements(items) LOOP
        IF jsonb_typeof(item) IS DISTINCT FROM 'object'
            OR jsonb_typeof(item->'name') IS DISTINCT FROM 'string'
            OR length(btrim(item->>'name')) NOT BETWEEN 1 AND 100
            OR item->>'name' <> btrim(item->>'name')
            OR jsonb_typeof(item->'amount') IS DISTINCT FROM 'string'
            OR (item->>'amount') !~ '^-?[0-9]{1,12}(\.[0-9]{1,2})?$'
        THEN RETURN false; END IF;
        cost := cost + (item->>'amount')::numeric;
    END LOOP;
    RETURN cost = total;
END $$;
ALTER TABLE expenses ADD CONSTRAINT expense_cost_breakdown_total
    CHECK (valid_expense_cost_breakdown(cost_breakdown, coalesce(base_amount, amount)));

INSERT INTO expense_settings(kind,category_id,name)
SELECT 'name',c.id,n.name FROM (VALUES
    ('Electrical issue'),('Towing'),('PM service'),('Truck wash'),('Detailing'),
    ('Road service'),('Body work'),('Leakage'),('Brake issue'),('Tire issue'),('Oil change')
) n(name) JOIN expense_settings c ON c.kind='category' AND lower(c.name)='maintenance' AND c.active
ON CONFLICT DO NOTHING;

COMMIT;
