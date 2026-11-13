package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type reportLoad struct {
	reportIdentity
	parent, trip, number, date, serviceDate, pickupDate, deliveryDate, boardSlot, state, note string
	gross, miles, loaded, empty                                                               *float64
	pickupOnTime, deliveryOnTime                                                              *bool
	pickupActual, pickupScheduled, deliveryActual, deliveryScheduled                          string
}

func (l reportLoad) evidence() ReportEvidence {
	kind, id := "Loads", l.parent
	if l.trip != "" {
		id += "/" + l.trip
	}
	if l.parent == "" {
		kind, id = "Gross Board", l.driver+"/"+l.serviceDate+"/"+l.boardSlot
	}
	values := map[string]float64{}
	for key, value := range map[string]*float64{"gross": l.gross, "miles": l.miles, "loadedMiles": l.loaded, "deadheadMiles": l.empty} {
		if value != nil {
			values[key] = *value
		}
	}
	return ReportEvidence{Kind: kind, ID: id, Label: l.number, Date: l.date, Note: l.note, State: l.state, Values: values,
		PickupActual: l.pickupActual, PickupScheduled: l.pickupScheduled, DeliveryActual: l.deliveryActual, DeliveryScheduled: l.deliveryScheduled}
}

func readReportLoads(ctx context.Context, tx pgx.Tx, view, from, to string) ([]reportLoad, error) {
	rows, err := tx.Query(ctx, reportsLoadsSQL, from, to, view == ReportDispatcher)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	loads := []reportLoad{}
	for rows.Next() {
		var l reportLoad
		if err = rows.Scan(&l.parent, &l.trip, &l.number, &l.date, &l.serviceDate, &l.pickupDate, &l.deliveryDate, &l.boardSlot, &l.state, &l.note,
			&l.gross, &l.miles, &l.loaded, &l.empty, &l.pickupOnTime, &l.deliveryOnTime, &l.pickupActual, &l.pickupScheduled, &l.deliveryActual, &l.deliveryScheduled,
			&l.truck, &l.unit, &l.driver, &l.name, &l.dispatcher, &l.dispatcherName, &l.employment); err != nil {
			return nil, err
		}
		loads = append(loads, l)
	}
	return loads, rows.Err()
}

const reportsBoardFrom = ` FROM ` + grossBoardEntriesSQL + ` e ` + grossBoardResolvedLoad + `
 LEFT JOIN gross_board_transfers g ON g.active AND (g.driver_id,g.source_date,g.source_slot)=(e.driver_id,e.service_date,e.slot)
 WHERE NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''`

var reportsLoadsSQL = `
WITH bounds AS (SELECT $1::date first,$2::date last,
 $1::date::timestamp AT TIME ZONE 'America/New_York' starts,($2::date+1)::timestamp AT TIME ZONE 'America/New_York' ends),
 board AS MATERIALIZED (
 SELECT e.*,l.id matched,g.dispatch_date,g.truck_id transfer_truck,g.dispatcher_id credited_id,g.dispatcher_name credited_name
 ` + reportsBoardFrom + `
 ),
 selected AS MATERIALIZED (
 SELECT l.* FROM loads l CROSS JOIN bounds b
 WHERE lower(btrim(l.status)) IN ('delivered','completed','invoiced')
 AND ((l.delivery_time>=b.starts AND l.delivery_time<b.ends)
 OR (l.delivery_time IS NULL AND coalesce(l.delivery_appointment_time,l.pickup_time,l.created_datetime)>=b.starts
 AND coalesce(l.delivery_appointment_time,l.pickup_time,l.created_datetime)<b.ends)
 OR l.id IN (SELECT matched FROM board WHERE dispatch_date BETWEEN b.first AND b.last OR service_date BETWEEN b.first AND b.last))
 ),
 allocations AS (
 SELECT l.*,s.trip_id,s.loaded,s.empty,s.gross,s.miles,s.is_split
 FROM selected l CROSS JOIN LATERAL (` + loadTripValidationSQL + `
 SELECT x.id trip_id, l.split_expected OR stats.n>1 is_split,
  CASE WHEN x.id IS NOT NULL THEN x.gross WHEN NOT l.split_expected AND stats.n<=1 AND (l.total_pay<>0 OR l.raw_payload->>'total_pay' ~ '^-?0+(\.0+)?$') THEN l.total_pay END gross,
  CASE WHEN x.id IS NOT NULL THEN x.loaded+x.empty WHEN NOT l.split_expected AND stats.n<=1 THEN l.total_miles END miles,
  CASE WHEN x.id IS NOT NULL THEN x.loaded WHEN NOT l.split_expected AND stats.n<=1
   AND single.v->>'mile' ~ '^[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (single.v->>'mile')::numeric END loaded,
  CASE WHEN x.id IS NOT NULL THEN x.empty WHEN NOT l.split_expected AND stats.n<=1
   AND single.v->>'empty_mile' ~ '^[0-9]{1,10}(\.[0-9]{1,2})?$' THEN (single.v->>'empty_mile')::numeric END empty
 FROM stats LEFT JOIN trips x ON stats.valid AND stats.n>1
 CROSS JOIN LATERAL (SELECT coalesce(l.raw_payload->'trips'->0,l.raw_payload->'trip') v) single) s
 ),
 board_ranked AS MATERIALIZED (
 SELECT e.*,count(*) OVER (PARTITION BY e.matched,e.load_trip_id)<>count(*) OVER (PARTITION BY e.matched,e.load_trip_id,e.driver_id)
  OR count(*) OVER (PARTITION BY e.matched,e.load_trip_id)<>count(*) OVER (PARTITION BY e.matched,e.load_trip_id,e.dispatch_date) conflict,
  row_number() OVER (PARTITION BY e.matched,e.load_trip_id ORDER BY e.service_date,e.slot,e.driver_id) ordinal
 FROM board e JOIN selected l ON l.id=e.matched
 ),
 sourced AS (
 SELECT a.id::text parent,coalesce(a.trip_id::text,'') trip_id,a.load_id,
  coalesce(b.service_date,(a.delivery_time AT TIME ZONE 'America/New_York')::date,
   (coalesce(a.delivery_appointment_time,a.pickup_time,a.created_datetime) AT TIME ZONE 'America/New_York')::date) service,
  CASE WHEN $3 AND b.dispatch_date IS NOT NULL AND NOT b.conflict THEN b.dispatch_date
   ELSE coalesce((a.delivery_time AT TIME ZONE 'America/New_York')::date,b.service_date,
   (coalesce(a.delivery_appointment_time,a.pickup_time,a.created_datetime) AT TIME ZONE 'America/New_York')::date) END report_date,
  a.pickup_time,a.delivery_time,a.pickup_appointment_time,a.delivery_appointment_time,coalesce(b.slot,0) slot,
  CASE WHEN availability.present=false OR a.delivery_time IS NULL OR (a.is_split AND a.trip_id IS NULL)
   THEN 'review' ELSE 'confirmed' END state,
  CASE WHEN availability.present=false THEN 'Load source missing'
   WHEN a.delivery_time IS NULL THEN 'Missing actual delivery time'
   WHEN a.is_split AND a.trip_id IS NULL THEN 'Unverified Loads trip allocation'
   WHEN b.conflict THEN 'Conflicting Gross Board assignments'
   WHEN b.dispatch_date IS NOT NULL THEN 'Gross Board transfer to '||b.dispatch_date::text ELSE '' END note,
  a.gross,a.miles,a.loaded,a.empty,
  CASE WHEN NOT coalesce(b.conflict,false) THEN coalesce(b.driver_id,CASE WHEN NOT a.is_split THEN a.driver_id END) END driver,
  CASE WHEN NOT a.is_split AND NOT coalesce(b.conflict,false) THEN a.truck_id END provider_truck,
  CASE WHEN NOT coalesce(b.conflict,false) THEN b.transfer_truck END transfer_truck,
  CASE WHEN NOT coalesce(b.conflict,false) THEN b.credited_id END credited_id,
  CASE WHEN NOT coalesce(b.conflict,false) THEN b.credited_name END credited_name
 FROM allocations a LEFT JOIN load_source_availability availability ON availability.load_id=a.id
 LEFT JOIN board_ranked b ON b.matched=a.id AND b.load_trip_id IS NOT DISTINCT FROM a.trip_id AND b.ordinal=1
 UNION ALL
 SELECT '','',e.load_number,e.service_date,CASE WHEN $3 THEN coalesce(e.dispatch_date,e.service_date) ELSE e.service_date END,
  NULL,NULL,NULL,NULL,e.slot,'provisional','Unmatched manual Gross Board entry',
  coalesce(e.entered_original_rate,e.original_rate),coalesce(e.entered_miles,e.miles),NULL,NULL,
  e.driver_id,NULL,e.transfer_truck,e.credited_id,e.credited_name FROM board e WHERE e.matched IS NULL
 ),
 in_period AS MATERIALIZED (SELECT x.* FROM sourced x,bounds b WHERE x.report_date BETWEEN b.first AND b.last),
 dates AS (SELECT coalesce(jsonb_agg(jsonb_build_object('driver',driver,'date',service)),'[]'::jsonb) refs
 FROM (SELECT DISTINCT driver,service FROM in_period WHERE driver IS NOT NULL) r),
 dims AS (SELECT a.* FROM dates CROSS JOIN LATERAL (` + strings.ReplaceAll(payrollDatedAssignmentsSQL, "$1", "dates.refs") + `)
  a(driver,service_day,truck,unit,dispatcher,dispatcher_name,employment)),
 linked AS (
 SELECT p.*,coalesce(p.transfer_truck,nullif(a.truck,'')::uuid,CASE WHEN p.driver IS NULL THEN p.provider_truck END) truck,
  coalesce(p.credited_id,a.dispatcher,'') dispatcher,coalesce(p.credited_name,a.dispatcher_name,'') dispatcher_name
 FROM in_period p LEFT JOIN dims a ON a.driver=p.driver::text AND a.service_day=p.service::text
 )
 SELECT p.parent,p.trip_id,p.load_id,p.report_date::text,p.service::text,
  coalesce((p.pickup_time AT TIME ZONE 'America/New_York')::date::text,''),
  coalesce((p.delivery_time AT TIME ZONE 'America/New_York')::date::text,''),p.slot::text,p.state,p.note,
  (p.gross*100)::float8,p.miles::float8,p.loaded::float8,p.empty::float8,
  p.pickup_time<=p.pickup_appointment_time,p.delivery_time<=p.delivery_appointment_time,
  coalesce(to_char(p.pickup_time AT TIME ZONE 'America/New_York','YYYY-MM-DD HH24:MI:SS'),''),coalesce(to_char(p.pickup_appointment_time AT TIME ZONE 'America/New_York','YYYY-MM-DD HH24:MI:SS'),''),
  coalesce(to_char(p.delivery_time AT TIME ZONE 'America/New_York','YYYY-MM-DD HH24:MI:SS'),''),coalesce(to_char(p.delivery_appointment_time AT TIME ZONE 'America/New_York','YYYY-MM-DD HH24:MI:SS'),''),
  coalesce(p.truck::text,''),coalesce(t.unit_number,''),coalesce(p.driver::text,''),coalesce(d.full_name,''),
  CASE WHEN p.dispatcher='' AND p.dispatcher_name NOT IN ('','Unassigned') THEN 'historical:'||p.dispatcher_name ELSE p.dispatcher END,p.dispatcher_name,
  CASE WHEN own.is_company THEN 'company' WHEN own.driver_id=payroll_driver_at(p.driver,p.service) THEN 'owner_operator'
   WHEN own.owner_id IS NOT NULL AND p.driver IS NOT NULL THEN 'investor_driver' ELSE 'unassigned' END
 FROM linked p LEFT JOIN trucks t ON t.id=p.truck LEFT JOIN drivers d ON d.id=p.driver
 ` + reportsOwnershipSQL("p.truck", "p.service")

// Use the same half-open New York ownership boundaries as CAT Scale.
func reportsOwnershipSQL(truck, day string) string {
	return ` LEFT JOIN LATERAL (
 SELECT (array_agg(DISTINCT h.owner_id))[1] owner_id
 FROM truck_ownership_history h WHERE h.truck_id=` + truck + `
 AND (NOT h.start_known OR h.assigned_at<((` + day + `+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=((` + day + `+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 HAVING count(DISTINCT h.owner_id)=1
 ) h ON true
 LEFT JOIN LATERAL (SELECT i.id owner_id,i.driver_id,i.is_company FROM investors i WHERE i.id=h.owner_id) own ON true`
}

func reportAssignments(ctx context.Context, tx pgx.Tx, keys map[reportAssignmentKey]bool) (map[reportAssignmentKey]reportIdentity, error) {
	refs := make([]reportAssignmentKey, 0, len(keys))
	for key := range keys {
		if key.Driver != "" && key.Date != "" {
			refs = append(refs, key)
		}
	}
	body, err := json.Marshal(refs)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, payrollDatedAssignmentsSQL, body)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[reportAssignmentKey]reportIdentity{}
	for rows.Next() {
		var key reportAssignmentKey
		var i reportIdentity
		if err := rows.Scan(&key.Driver, &key.Date, &i.truck, &i.unit, &i.dispatcher, &i.dispatcherName, &i.employment); err != nil {
			return nil, err
		}
		i.driver = key.Driver
		if i.dispatcher == "" && i.dispatcherName != "" && i.dispatcherName != "Unassigned" {
			i.dispatcher = "historical:" + i.dispatcherName
		}
		result[key] = i
	}
	return result, rows.Err()
}

type reportAssignmentKey struct {
	Driver string `json:"driver"`
	Date   string `json:"date"`
}

func reportTripID(trip *int) string {
	if trip == nil {
		return ""
	}
	return strconv.Itoa(*trip)
}
