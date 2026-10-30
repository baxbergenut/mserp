BEGIN;
-- Earlier removals stay authoritative for their own weeks even after an extra
-- load is added and removed elsewhere. No amounts or decisions are rewritten.
CREATE INDEX payroll_load_removed_weeks ON payroll_load_placement_history
 ((after_value->>'load_record_id'),(after_value->>'load_trip_id'),(after_value->>'week_start'))
 WHERE action='remove';
COMMIT;
