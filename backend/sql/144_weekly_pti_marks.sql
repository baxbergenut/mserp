BEGIN;
ALTER TABLE weekly_pti_rows ADD CONSTRAINT weekly_pti_mark_values
 CHECK (NOT (data ? 'mark') OR (jsonb_typeof(data->'mark')='string' AND data->>'mark' IN ('/','1','0')));
COMMIT;
