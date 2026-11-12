-- Read-only Status Board lookup: find the latest departure from an exact
-- driver/source pair without decoding that driver's entire audit history.
-- Existing history and all financial/operational write guards are unchanged.
CREATE INDEX IF NOT EXISTS driver_board_history_departure_idx
 ON driver_board_history(driver_id,
   (((before_values->>'loadPlan')::jsonb->'current'->>'loadId')),id DESC)
 WHERE before_values ? 'loadPlan' AND after_values ? 'loadPlan'
   AND (before_values->>'loadPlan')::jsonb->'current'->>'loadId' IS DISTINCT FROM
       (after_values->>'loadPlan')::jsonb->'current'->>'loadId';
