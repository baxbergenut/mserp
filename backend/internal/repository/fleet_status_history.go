package repository

// Operator corrections append a replacement event referencing the original ID.
// Keep original events immutable, but let the reviewed earlier date supersede
// unknown activation baselines it crosses. Callers bound status_history to their
// snapshot first, so pagination cannot see a correction recorded after its cursor.
const effectiveFleetStatusHistoryCTE = `effective_status_history AS MATERIALIZED (
 SELECT h.* FROM status_history h WHERE NOT EXISTS (
 SELECT 1 FROM status_history correction
 WHERE correction.source='operator_correction' AND NOT correction.baseline
 AND correction.entity_kind=h.entity_kind AND correction.entity_id=h.entity_id AND correction.track=h.track
 AND (correction.recorded_at,correction.id)>(h.recorded_at,h.id)
 AND (correction.source_ref=h.id::text OR (h.baseline AND correction.effective_at<=h.effective_at))
 ))`
