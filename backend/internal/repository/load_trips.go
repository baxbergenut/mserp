package repository

import "strconv"

func loadAllocationKey(load int, trip *int) string {
	key := strconv.Itoa(load)
	if trip != nil {
		key += ":" + strconv.Itoa(*trip)
	}
	return key
}

// GrossBoardTrip retains provider amounts as evidence, separately from reviewed revenue.
// IDs are scoped by the parent load. Names are display-only; selection is explicit.
type GrossBoardTrip struct {
	ID           int    `json:"id"`
	Number       string `json:"number"`
	DriverName   string `json:"driverName"`
	TruckUnit    string `json:"truckUnit"`
	OriginalRate string `json:"originalRate"` // Provider amount; legacy wire name.
	ReviewedRate string `json:"reviewedRate,omitempty"`
	Miles        string `json:"miles"`
	LoadedMiles  string `json:"loadedMiles"`
	EmptyMiles   string `json:"emptyMiles"`
	StartDate    string `json:"startDate"`
	EndDate      string `json:"endDate"`
}

const loadTripFieldsSQL = `v,
    CASE WHEN v->>'id' ~ '^[1-9][0-9]{0,8}$' THEN (v->>'id')::integer END id,
    CASE WHEN v->>'total_load_pay' ~ '^-?[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (v->>'total_load_pay')::numeric END gross,
    CASE WHEN v->>'mile' ~ '^[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (v->>'mile')::numeric END loaded,
    CASE WHEN v->>'empty_mile' ~ '^[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (v->>'empty_mile')::numeric END empty`

const loadTripOptionSQL = `jsonb_build_object('id',id,'number',coalesce(v->>'trip_id',''),
      'driverName',coalesce(v->>'driver__full_name',''),'truckUnit',coalesce(v->>'truck__unit_number',''),
      'originalRate',coalesce(gross::text,''),'miles',coalesce((loaded+empty)::text,''),
      'loadedMiles',coalesce(loaded::text,''),'emptyMiles',coalesce(empty::text,''),
      'startDate',coalesce(v->>'trip_begin_date',''),'endDate',coalesce(v->>'trip_end_date',''))`

// Shared projection for board, payroll and dashboard. Missing/invalid allocations
// never fall back to the full order.
const loadTripValidationSQL = `WITH trips AS (
   SELECT ` + loadTripFieldsSQL + `
   FROM jsonb_array_elements(CASE WHEN jsonb_typeof(l.raw_payload->'trips')='array'
    THEN l.raw_payload->'trips' ELSE '[]'::jsonb END) v
  ), stats AS (
   SELECT count(*) n, count(id)=count(*) AND count(DISTINCT id)=count(*)
    AND count(gross)=count(*) AND count(loaded)=count(*) AND count(empty)=count(*)
    AND bool_and(btrim(coalesce(v->>'driver__full_name',''))<>'')
    AND sum(gross)=l.total_pay AND sum(loaded+empty)=l.total_miles AS valid,
    count(id)=count(*) AND count(DISTINCT id)=count(*) AND bool_and(btrim(coalesce(v->>'driver__full_name',''))<>'') identities_valid,
    count(gross)=count(*) AND count(loaded)=count(*) AND count(empty)=count(*) amounts_valid,
    sum(gross) gross_total,sum(loaded+empty) miles_total,
    coalesce(jsonb_agg(` + loadTripOptionSQL + ` ORDER BY id),'[]'::jsonb) options
   FROM trips
  ), decision AS (
   SELECT nullif(l.split_allocation,'null'::jsonb) saved,
    md5(jsonb_build_object('total',l.total_pay,'miles',l.total_miles,'trips',stats.options)::text) source_hash
   FROM stats
  ), checked AS (
   SELECT decision.*,
    CASE WHEN saved IS NOT NULL THEN coalesce(stats.identities_valid AND stats.n>1
      AND stats.amounts_valid AND stats.miles_total=l.total_miles
      AND saved->>'sourceHash'=source_hash
      AND (SELECT sum((v->>'originalRate')::numeric) FROM jsonb_array_elements(saved->'allocations') v)=l.total_pay,false)
     ELSE coalesce(stats.valid,false) END valid
   FROM decision,stats
  )`

const loadTripSourceSQL = `
 LEFT JOIN LATERAL (` + loadTripValidationSQL + `
 SELECT coalesce(l.split_expected,false) OR stats.n>1 OR e.load_trip_id IS NOT NULL AS split,
   CASE WHEN stats.identities_valid THEN
     CASE WHEN e.load_trip_id IS NOT NULL THEN selected.gross
       WHEN stats.n=1 AND NOT coalesce(l.split_expected,false) THEN (SELECT gross FROM trips LIMIT 1) END
   END AS driver_gross,
   (coalesce(l.split_expected,false) OR stats.n>1 OR e.load_trip_id IS NOT NULL) AND (NOT checked.valid OR selected.id IS NULL) AS review,
   CASE WHEN coalesce(l.split_expected,false) OR stats.n>1 OR e.load_trip_id IS NOT NULL THEN CASE WHEN checked.valid THEN coalesce((allocation.v->>'originalRate')::numeric,selected.gross) END ELSE l.total_pay END original,
   CASE WHEN coalesce(l.split_expected,false) OR stats.n>1 OR e.load_trip_id IS NOT NULL THEN CASE WHEN checked.valid THEN selected.loaded+selected.empty END ELSE l.total_miles END miles,
   CASE WHEN checked.saved IS NULL THEN stats.options ELSE
    coalesce((SELECT jsonb_agg(o || jsonb_build_object('reviewedRate',coalesce(a.v->>'originalRate','')) ORDER BY (o->>'id')::integer)
     FROM jsonb_array_elements(stats.options) o LEFT JOIN LATERAL
      (SELECT av.value AS v FROM jsonb_array_elements(checked.saved->'allocations') av(value) WHERE av.value->>'tripId'=o->>'id') a ON true),'[]'::jsonb) END options,
   selected.v selected_trip, checked.saved IS NOT NULL AS reviewed,checked.source_hash,
   coalesce((checked.saved->>'version')::integer,0) allocation_version,
   CASE WHEN NOT (coalesce(l.split_expected,false) OR stats.n>1 OR e.load_trip_id IS NOT NULL) OR checked.valid THEN ''
    WHEN checked.saved IS NOT NULL AND checked.saved->>'sourceHash' IS DISTINCT FROM checked.source_hash THEN 'DataTruck changed after this split was reviewed. The saved allocation is retained; review it again before using it.'
    WHEN stats.n=0 THEN 'Trip allocations are missing. Refresh the source load before selecting a trip.'
    WHEN NOT coalesce(stats.identities_valid,false) THEN 'Trip IDs or driver names are missing or duplicated. Correct the source trips before selecting.'
    WHEN NOT stats.amounts_valid THEN 'Trip gross or mileage is missing or invalid. Correct the source trips before selecting.'
    WHEN stats.gross_total<>l.total_pay THEN 'Trip gross totals $'||to_char(stats.gross_total,'FM999,999,999,990.00')||' but the load total is $'||to_char(l.total_pay,'FM999,999,999,990.00')||'. Review the original gross split independently of DataTruck trip amounts.'
    WHEN stats.miles_total<>l.total_miles THEN 'Trip miles total '||to_char(stats.miles_total,'FM999,999,999,990.00')||' but the load total is '||to_char(l.total_miles,'FM999,999,999,990.00')||'. Correct the DataTruck mileage before selecting a trip.'
    ELSE 'Trip allocations cannot be verified. Correct the source trips before selecting.' END review_reason
  FROM stats CROSS JOIN checked LEFT JOIN LATERAL (SELECT * FROM trips WHERE id=e.load_trip_id LIMIT 1) selected ON true
  LEFT JOIN LATERAL (SELECT av.value AS v FROM jsonb_array_elements(checked.saved->'allocations') av(value) WHERE av.value->>'tripId'=e.load_trip_id::text) allocation ON true
 ) trip_source ON true `

// With at most one ordinary trip, parent amounts remain authoritative. Keep
// its driver gross and picker evidence using the same parsing as the full
// split validator. Fence its input so only selected or complex trips expand it.
const ordinaryBoardTripSQL = `e.load_trip_id IS NULL AND NOT coalesce(l.split_expected,false) AND l.split_allocation IS NULL AND (src.trips->1) IS NULL`

const boardTripSourceSQL = ` LEFT JOIN LATERAL (SELECT l.raw_payload->'trips' trips OFFSET 0) src ON true
 LEFT JOIN LATERAL (
 SELECT false AS split,CASE WHEN id IS NOT NULL AND btrim(coalesce(v->>'driver__full_name',''))<>'' THEN gross END AS driver_gross,
 false AS review,l.total_pay AS original,l.total_miles AS miles,CASE WHEN v IS NULL THEN '[]'::jsonb ELSE jsonb_build_array(` + loadTripOptionSQL + `) END AS options,false AS reviewed,''::text AS review_reason
 FROM (SELECT CASE WHEN jsonb_typeof(src.trips)='array' THEN src.trips->0 END AS data WHERE ` + ordinaryBoardTripSQL + ` OFFSET 0) item
 CROSS JOIN LATERAL (SELECT ` + loadTripFieldsSQL + ` FROM (SELECT item.data AS v) raw OFFSET 0) parsed
 UNION ALL
 SELECT trip_source.split,trip_source.driver_gross,trip_source.review,trip_source.original,
 trip_source.miles,trip_source.options,trip_source.reviewed,trip_source.review_reason
 FROM (SELECT l.* WHERE NOT (` + ordinaryBoardTripSQL + `) OFFSET 0) l ` + loadTripSourceSQL + `
 ) trip_source ON true `
