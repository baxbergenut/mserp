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

// Shared projection for board, payroll and dashboard. Missing/invalid allocations
// never fall back to the full order.
const loadTripValidationSQL = `WITH trips AS (
   SELECT v,
    CASE WHEN v->>'id' ~ '^[1-9][0-9]{0,8}$' THEN (v->>'id')::integer END id,
    CASE WHEN v->>'total_load_pay' ~ '^-?[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (v->>'total_load_pay')::numeric END gross,
    CASE WHEN v->>'mile' ~ '^[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (v->>'mile')::numeric END loaded,
    CASE WHEN v->>'empty_mile' ~ '^[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (v->>'empty_mile')::numeric END empty
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
    coalesce(jsonb_agg(jsonb_build_object('id',id,'number',coalesce(v->>'trip_id',''),
      'driverName',coalesce(v->>'driver__full_name',''),'truckUnit',coalesce(v->>'truck__unit_number',''),
      'originalRate',coalesce(gross::text,''),'miles',coalesce((loaded+empty)::text,''),
      'loadedMiles',coalesce(loaded::text,''),'emptyMiles',coalesce(empty::text,''),
      'startDate',coalesce(v->>'trip_begin_date',''),'endDate',coalesce(v->>'trip_end_date','')) ORDER BY id),'[]'::jsonb) options
   FROM trips
  ), decision AS (
   SELECT nullif(to_jsonb(l)->'split_allocation','null'::jsonb) saved,
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
