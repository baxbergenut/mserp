-- Read-only report access paths. No source, settlement or protocol changes.
CREATE INDEX IF NOT EXISTS reports_loads_delivery ON loads(delivery_time,id)
 WHERE lower(btrim(status)) IN ('delivered','completed','invoiced');
CREATE OR REPLACE FUNCTION reports_statement_load_ids(statement jsonb) RETURNS bigint[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT coalesce(array_agg(DISTINCT (item->>'loadRecordId')::bigint),'{}'::bigint[])
 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(statement->'loads')='array' THEN statement->'loads' ELSE '[]'::jsonb END) item
 WHERE item->>'loadRecordId' ~ '^[0-9]{1,10}$'
$$;
CREATE INDEX IF NOT EXISTS reports_frozen_load_ids ON payroll_settlements
 USING gin (reports_statement_load_ids(report)) WHERE finalized;
