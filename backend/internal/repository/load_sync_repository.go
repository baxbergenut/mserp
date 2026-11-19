package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// OperationalLoadIDs deduplicates the local service-date window before spending
// API requests. Recent creations include orders whose stops/dates are still being
// entered. Current selections and explicitly restored plans stay eligible
// regardless of their age.
func (r *LoadRepository) OperationalLoadIDs(ctx context.Context, from, until time.Time) ([]int, error) {
	rows, err := r.pool.Query(ctx, `WITH current_ids AS (
 SELECT l.id FROM `+boardPlansSQL+` e
 JOIN driver_board_load_state s ON s.driver_id=e.driver_id AND s.payload->'current'->>'planId'=e.plan_id::text
 JOIN drivers d ON d.id=e.driver_id AND d.active
 `+grossBoardResolvedLoad+`
 WHERE NOT e.deleted AND e.day_status='' AND l.id IS NOT NULL
 UNION
 SELECT l.id FROM driver_board_load_state s JOIN drivers d ON d.id=s.driver_id AND d.active
 JOIN loads l ON l.id::text=s.payload->'current'->>'loadId'
 UNION
 SELECT l.id FROM driver_board b JOIN drivers d ON d.id=b.driver_id AND d.active
 JOIN loads l ON lower(btrim(l.load_id))=lower(btrim(b.current_load)) WHERE btrim(b.current_load)<>''
 UNION
 SELECT l.id FROM driver_board_load_state s JOIN drivers d ON d.id=s.driver_id AND d.active
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.payload->'manual','[]'::jsonb)) p
 JOIN loads l ON lower(btrim(l.load_id))=lower(btrim(p->>'number'))
 WHERE NOT coalesce(s.payload->'hidden' ? (p->>'planId'),false)
 AND NOT coalesce(s.payload->'completed' ? ('plan:'||(p->>'planId')),false)
 AND lower(btrim(l.status)) NOT IN ('delivered','completed','invoiced','cancelled','canceled')
 UNION
 SELECT l.id FROM driver_board_load_state s JOIN drivers d ON d.id=s.driver_id AND d.active
 CROSS JOIN LATERAL jsonb_object_keys(coalesce(s.payload->'restoredPlans','{}'::jsonb)) restored(plan_id)
 JOIN `+boardPlansSQL+` e ON e.driver_id=s.driver_id AND e.plan_id=restored.plan_id::uuid
 `+grossBoardResolvedLoad+`
 WHERE NOT e.deleted AND e.day_status='' AND l.id IS NOT NULL
 AND NOT coalesce(s.payload->'hidden' ? e.plan_id::text,false)
 AND NOT coalesce(s.payload->'completed' ? ('load:'||l.id::text),false)
 AND NOT coalesce(s.payload->'completed' ? ('plan:'||e.plan_id::text),false)
 AND lower(btrim(l.status)) NOT IN ('delivered','completed','invoiced','cancelled','canceled')
 )
 SELECT l.id FROM loads l WHERE
 (l.pickup_time >= $1 AND l.pickup_time < $2) OR
 (l.pickup_appointment_time >= $1 AND l.pickup_appointment_time < $2) OR
 (l.delivery_time >= $1 AND l.delivery_time < $2) OR
 (l.delivery_appointment_time >= $1 AND l.delivery_appointment_time < $2) OR
 (l.created_datetime >= $1 AND l.created_datetime < $2) OR
 l.id IN (SELECT id FROM current_ids)
 ORDER BY CASE WHEN l.id IN (SELECT id FROM current_ids) THEN 0 ELSE 1 END, l.id`, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RefreshLoads updates existing source records only. Frequent refreshes must not
// replay historical driver/truck assignments or advance the discovery watermark.
func (r *LoadRepository) RefreshLoads(ctx context.Context, records []LoadRecord) error {
	return r.refreshLoads(ctx, records, false)
}

// ReconcileLoads also recovers missing historical records below the discovery
// watermark. The caller must cap the upstream scan at that watermark.
func (r *LoadRepository) ReconcileLoads(ctx context.Context, records []LoadRecord) error {
	return r.refreshLoads(ctx, records, true)
}

func (r *LoadRepository) refreshLoads(ctx context.Context, records []LoadRecord, recoverMissing bool) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := beginPayrollSourceMutation(ctx, r.pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	records, err = currentLoadSourceRecords(ctx, tx, records)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	if err = refreshLoadRows(ctx, tx, records, recoverMissing); err != nil {
		return err
	}
	if err := saveLoadSourceVersions(ctx, tx, records); err != nil {
		return err
	}
	ids := make([]int, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	if _, err := tx.Exec(ctx, `UPDATE load_source_availability SET present=true,checked_at=now() WHERE load_id=ANY($1::int[]) AND NOT present`, ids); err != nil {
		return err
	}
	if err = fillPendingDriverGross(ctx, tx, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type loadRefreshNames struct {
	drivers, dispatchers map[string]string
}

// Resolve exact names once for a batch. Historical profiles retain their
// original created-at/ID precedence, including inactive drivers.
func loadRefreshRoster(ctx context.Context, tx pgx.Tx, records []LoadRecord) (loadRefreshNames, error) {
	names := loadRefreshNames{drivers: map[string]string{}, dispatchers: map[string]string{}}
	var drivers, dispatchers []string
	for _, r := range records {
		if r.DriverName != nil {
			drivers = append(drivers, normalizeName(*r.DriverName))
		}
		if r.DispatcherName != nil {
			dispatchers = append(dispatchers, normalizeName(*r.DispatcherName))
		}
	}
	if len(drivers)+len(dispatchers) == 0 {
		return names, nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (kind,normalized_name) kind,normalized_name,id FROM (
 SELECT 'driver' kind,normalized_name,id,created_at FROM drivers WHERE normalized_name=ANY($1::text[])
 UNION ALL SELECT 'dispatcher',normalized_name,id,created_at FROM dispatchers WHERE normalized_name=ANY($2::text[])
 ) p ORDER BY kind,normalized_name,created_at,id`, drivers, dispatchers)
	if err != nil {
		return names, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, name, id string
		if err := rows.Scan(&kind, &name, &id); err != nil {
			return names, err
		}
		if kind == "driver" {
			names.drivers[name] = id
		} else {
			names.dispatchers[name] = id
		}
	}
	return names, rows.Err()
}

// pgx needs the PostgreSQL types of VALUES parameters before execution. Numeric
// precision matches the destination columns, including comparison before writes.
var loadRefreshTypes = []string{"integer", "text", "uuid", "uuid", "text", "text", "numeric(10,2)", "numeric(10,2)", "numeric(10,2)", "numeric(10,2)", "numeric(10,4)", "text", "text", "text", "text", "text", "timestamptz", "timestamptz", "timestamptz", "timestamptz", "timestamptz", "timestamptz", "jsonb"}

const refreshExistingLoadsSQL = `UPDATE loads l SET
 load_id=r.load_id,driver_id=r.driver_id,dispatcher_id=r.dispatcher_id,shipment_id=r.shipment_id,status=r.status,
 load_pay=r.load_pay,total_other_pay=r.total_other_pay,total_pay=r.total_pay,total_miles=r.total_miles,per_mile_revenue=r.per_mile_revenue,
 dispatcher_name=r.dispatcher_name,driver_name=r.driver_name,team_driver_name=r.team_driver_name,truck_unit=r.truck_unit,customer_name=r.customer_name,
 pickup_time=r.pickup_time,delivery_time=r.delivery_time,pickup_appointment_time=r.pickup_appointment_time,delivery_appointment_time=r.delivery_appointment_time,
 created_datetime=r.created_datetime,synced_at=r.synced_at,raw_payload=r.raw_payload
 FROM incoming r WHERE l.id=r.id AND (
 ROW(l.load_id,l.driver_id,l.dispatcher_id,l.shipment_id,l.status,l.load_pay,l.total_other_pay,l.total_pay,l.total_miles,l.per_mile_revenue,
 l.dispatcher_name,l.driver_name,l.team_driver_name,l.truck_unit,l.customer_name,l.pickup_time,l.delivery_time,l.pickup_appointment_time,l.delivery_appointment_time,l.created_datetime,l.raw_payload)
 IS DISTINCT FROM ROW(r.load_id,r.driver_id,r.dispatcher_id,r.shipment_id,r.status,r.load_pay,r.total_other_pay,r.total_pay,r.total_miles,r.per_mile_revenue,
 r.dispatcher_name,r.driver_name,r.team_driver_name,r.truck_unit,r.customer_name,r.pickup_time,r.delivery_time,r.pickup_appointment_time,r.delivery_appointment_time,r.created_datetime,r.raw_payload)
 OR l.synced_at < r.synced_at - interval '15 minutes')`

func refreshLoadRows(ctx context.Context, tx pgx.Tx, records []LoadRecord, recoverMissing bool) error {
	names, err := loadRefreshRoster(ctx, tx, records)
	if err != nil {
		return err
	}
	compatible := map[string]string{}
	var roster []driverNameCandidate
	for len(records) > 0 {
		var values strings.Builder
		args := make([]any, 0, min(64, len(records))*len(loadRefreshTypes))
		seen := map[int]bool{}
		for _, l := range records[:min(64, len(records))] {
			// Keep repeated observations of one load in input order.
			if seen[l.ID] {
				break
			}
			seen[l.ID] = true
			var driverID, dispatcherID *string
			if l.DriverName != nil {
				id := names.drivers[normalizeName(*l.DriverName)]
				if id == "" {
					var cached bool
					id, cached = compatible[*l.DriverName]
					if !cached {
						if roster == nil {
							roster, err = driverNameRoster(ctx, tx)
							if err != nil {
								return err
							}
						}
						id, _, _, err = matchCompatibleDriver(*l.DriverName, roster)
						if err != nil {
							return err
						}
						compatible[*l.DriverName] = id
					}
				}
				if id != "" {
					driverID = &id
				}
			}
			if l.DispatcherName != nil {
				if id := names.dispatchers[normalizeName(*l.DispatcherName)]; id != "" {
					dispatcherID = &id
				}
			}
			if values.Len() > 0 {
				values.WriteByte(',')
			}
			values.WriteByte('(')
			for n, typ := range loadRefreshTypes {
				if n > 0 {
					values.WriteByte(',')
				}
				fmt.Fprintf(&values, "$%d::%s", len(args)+n+1, typ)
			}
			values.WriteByte(')')
			args = append(args, l.ID, l.LoadID, driverID, dispatcherID, l.ShipmentID, l.Status, l.LoadPay, l.TotalOtherPay, l.TotalPay,
				l.TotalMiles, l.PerMileRevenue, l.DispatcherName, l.DriverName, l.TeamDriverName, l.TruckUnit, l.CustomerName,
				l.PickupTime, l.DeliveryTime, l.PickupAppointmentTime, l.DeliveryAppointmentTime, l.CreatedDatetime, l.SyncedAt, l.RawPayload)
		}
		query := "WITH incoming(" + loadImportColumns + ") AS (VALUES " + values.String() + ") " + refreshExistingLoadsSQL
		if recoverMissing {
			query = "INSERT INTO loads (" + loadImportColumns + ") VALUES " + values.String() + loadImportConflictSQL
		}
		if _, err = tx.Exec(ctx, query, args...); err != nil {
			return err
		}
		records = records[len(seen):]
	}
	return nil
}
