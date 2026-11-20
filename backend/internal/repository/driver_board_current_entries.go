package repository

// Rank only identities present on the displayed board. The first placement is
// determined by native source date and slot, including transfers and older
// number-only matches for the same driver, load and trip. A dispatch transfer
// moves Saturday or Sunday to the following Monday (gross_board_transfers).
var driverBoardCurrentEntries = `WITH displayed AS MATERIALIZED (
 SELECT e.*,l.id matched_id FROM ` + dispatchBoardEntriesSQL + ` e ` + grossBoardResolvedLoad + `
 WHERE e.service_date >= $1::date AND e.service_date < $1::date+7 AND e.source_date < $1::date+7
 AND e.source_date >= $1::date-2
), first_placement AS (
 SELECT DISTINCT ON (e.driver_id,k.matched_id,e.load_trip_id)
 e.driver_id,k.matched_id,e.load_trip_id,e.source_date,e.source_slot
 FROM (SELECT DISTINCT driver_id,matched_id FROM displayed WHERE matched_id IS NOT NULL) k
 JOIN ` + dispatchBoardEntriesSQL + ` e ON e.driver_id=k.driver_id
 AND ` + boardLoadIdentityStart + boardLoadIdentityFinish + `=k.matched_id
 WHERE NOT e.deleted AND e.day_status='' AND e.source_date < $1::date+7
 ORDER BY e.driver_id,k.matched_id,e.load_trip_id,e.source_date,e.source_slot
), matched_entries AS (
 SELECT d.*,coalesce((f.source_date,f.source_slot)<(d.source_date,d.source_slot),false) duplicate
 FROM displayed d LEFT JOIN first_placement f ON f.driver_id=d.driver_id AND f.matched_id=d.matched_id
 AND f.load_trip_id IS NOT DISTINCT FROM d.load_trip_id
), effective AS (
 SELECT e.*,coalesce(l.status,'') source_status,CASE WHEN l.id IS NOT NULL THEN l.load_id ELSE e.load_number END display_number,
 CASE WHEN trip_source.review THEN NULL ELSE CASE WHEN trip_source.reviewed THEN trip_source.original ELSE coalesce(e.entered_original_rate,CASE WHEN l.id IS NOT NULL THEN trip_source.original ELSE e.original_rate END) END END effective_original,
 CASE WHEN trip_source.review THEN NULL ELSE coalesce(e.entered_miles,CASE WHEN l.id IS NOT NULL THEN trip_source.miles ELSE e.miles END) END effective_miles,
 trip_source.original system_original,trip_source.miles system_miles,trip_source.driver_gross system_driver,
 trip_source.split,trip_source.review trip_review,trip_source.review_reason trip_review_reason,trip_source.options trips
 FROM matched_entries e LEFT JOIN loads l ON l.id=e.matched_id ` + boardTripSourceSQL + `
) `
