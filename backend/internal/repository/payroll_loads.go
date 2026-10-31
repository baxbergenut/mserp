package repository

// Match each relevant number once. Search all records for that number so an
// older duplicate remains ambiguous; never impose a history cutoff on identity.
var payrollLoadMatchCTE = `pay_entries AS MATERIALIZED (
 SELECT * FROM ` + payrollBoardEntriesSQL + ` e WHERE NOT e.deleted
 AND (e.payroll_supplement OR (e.source_date >= $1::date-7 AND e.source_date < $1::date+7))
 AND e.service_date >= $1::date AND e.service_date < $1::date+7
 AND ($3::uuid[] IS NULL OR e.driver_id=ANY($3::uuid[]))
), pay_load_numbers AS MATERIALIZED (
 SELECT n.number,CASE WHEN count(*)=1 THEN min(l.id) END id FROM (
 SELECT DISTINCT lower(btrim(load_number)) number FROM pay_entries
 WHERE load_record_id IS NULL AND load_number<>'' AND day_status=''
 ) n JOIN loads l ON lower(btrim(l.load_id))=n.number
 GROUP BY n.number
)`

var payrollEntriesCTE = payrollLoadMatchCTE + `, pay_loaded AS MATERIALIZED (
 SELECT l.*,facts.n trip_count,facts.valid trips_valid,facts.saved trip_allocation,facts.trips trip_values
 FROM loads l JOIN (SELECT DISTINCT coalesce(e.load_record_id,n.id) id
 FROM pay_entries e LEFT JOIN pay_load_numbers n ON n.number=lower(btrim(e.load_number)) WHERE e.day_status='') ids ON ids.id=l.id
 LEFT JOIN LATERAL (
 ` + loadTripValidationSQL + `
 SELECT stats.n,checked.valid,checked.saved,
 (SELECT jsonb_object_agg(id::text,jsonb_build_object('v',v,'gross',gross,'loaded',loaded,'empty',empty)) FROM trips WHERE id IS NOT NULL) trips
 FROM stats,checked
 ) facts ON true
)`

const payrollResolvedLoad = `
 LEFT JOIN pay_load_numbers matched ON matched.number=lower(btrim(e.load_number))
 LEFT JOIN pay_loaded l ON l.id=coalesce(e.load_record_id,matched.id) AND NOT e.deleted AND e.day_status=''
 LEFT JOIN LATERAL (
 SELECT coalesce(l.split_expected,false) OR l.trip_count>1 OR e.load_trip_id IS NOT NULL AS split,
 (coalesce(l.split_expected,false) OR l.trip_count>1 OR e.load_trip_id IS NOT NULL) AND (NOT l.trips_valid OR selected.v IS NULL) AS review,
 CASE WHEN coalesce(l.split_expected,false) OR l.trip_count>1 OR e.load_trip_id IS NOT NULL THEN
  CASE WHEN l.trips_valid THEN coalesce((allocation.v->>'originalRate')::numeric,(selected.v->>'gross')::numeric) END ELSE l.total_pay END original,
 CASE WHEN coalesce(l.split_expected,false) OR l.trip_count>1 OR e.load_trip_id IS NOT NULL THEN
  CASE WHEN l.trips_valid THEN (selected.v->>'loaded')::numeric+(selected.v->>'empty')::numeric END ELSE l.total_miles END miles,
 selected.v->'v' selected_trip,l.trip_allocation IS NOT NULL AS reviewed
 FROM (SELECT l.trip_values->e.load_trip_id::text v) selected
 LEFT JOIN LATERAL (SELECT av.value v FROM jsonb_array_elements(l.trip_allocation->'allocations') av(value) WHERE av.value->>'tripId'=e.load_trip_id::text) allocation ON true
 ) trip_source ON true `

// Keep the original projection available for independent calculation parity.
const payrollAuthoritativeResolvedLoad = `
 LEFT JOIN pay_load_numbers matched ON matched.number=lower(btrim(e.load_number))
 LEFT JOIN loads l ON l.id=coalesce(e.load_record_id,matched.id) AND NOT e.deleted AND e.day_status='' ` + loadTripSourceSQL
