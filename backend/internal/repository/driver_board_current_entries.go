package repository

// Keep historical identity matching for the exact same duplicate semantics as
// Gross Board, but hydrate trip allocations only for the displayed week. The
// Status Board needs neither historical trip details nor opening balances.
var driverBoardCurrentEntries = `WITH matched_entries AS MATERIALIZED (
 SELECT e.*,l.id matched_id,
 l.id IS NOT NULL AND row_number() OVER (
 PARTITION BY e.driver_id,l.id,e.load_trip_id ORDER BY e.source_date,e.source_slot)>1 duplicate
 FROM ` + dispatchBoardEntriesSQL + ` e ` + grossBoardResolvedLoad + `
 WHERE e.source_date < $1::date+7
), resolved AS (
 SELECT e.*,CASE WHEN l.id IS NOT NULL THEN l.load_id ELSE e.load_number END display_number,
 CASE WHEN trip_source.review THEN NULL ELSE CASE WHEN trip_source.reviewed THEN trip_source.original ELSE coalesce(e.entered_original_rate,CASE WHEN l.id IS NOT NULL THEN trip_source.original ELSE e.original_rate END) END END effective_original,
 CASE WHEN trip_source.review THEN NULL ELSE coalesce(e.entered_miles,CASE WHEN l.id IS NOT NULL THEN trip_source.miles ELSE e.miles END) END effective_miles,
 trip_source.original system_original,trip_source.miles system_miles,
 trip_source.split,trip_source.review trip_review,trip_source.review_reason trip_review_reason,trip_source.options trips
 FROM matched_entries e LEFT JOIN loads l ON l.id=e.matched_id ` + loadTripSourceSQL + `
 WHERE e.service_date >= $1::date AND e.service_date < $1::date+7
), effective AS (SELECT * FROM resolved) `
