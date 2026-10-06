package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// OperationalLoadIDs deduplicates the local service-date window before spending
// API requests. Recent creations include orders whose stops/dates are still being
// entered. Explicit current selections remain eligible regardless of their age.
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for _, l := range records {
		var driverID, dispatcherID *string
		if l.DriverName != nil {
			var id string
			err := tx.QueryRow(ctx, `SELECT id FROM drivers WHERE normalized_name=$1 ORDER BY created_at,id LIMIT 1`, normalizeName(*l.DriverName)).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				var found bool
				id, _, found, err = findCompatibleDriver(ctx, tx, *l.DriverName)
				if found {
					driverID = &id
				}
			} else if err == nil {
				driverID = &id
			}
			if err != nil {
				return err
			}
		}
		if l.DispatcherName != nil {
			var id string
			err := tx.QueryRow(ctx, `SELECT id FROM dispatchers WHERE normalized_name=$1 ORDER BY created_at,id LIMIT 1`, normalizeName(*l.DispatcherName)).Scan(&id)
			if err == nil {
				dispatcherID = &id
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		query := `UPDATE loads SET load_id=$2,driver_id=$3,dispatcher_id=$4,shipment_id=$5,status=$6,
 load_pay=$7,total_other_pay=$8,total_pay=$9,total_miles=$10,per_mile_revenue=$11,
 dispatcher_name=$12,driver_name=$13,team_driver_name=$14,truck_unit=$15,customer_name=$16,
 pickup_time=$17,delivery_time=$18,pickup_appointment_time=$19,delivery_appointment_time=$20,
 created_datetime=$21,synced_at=$22,raw_payload=$23 WHERE id=$1`
		if recoverMissing {
			query = upsertLoadSQL
		}
		batch.Queue(query,
			l.ID, l.LoadID, driverID, dispatcherID, l.ShipmentID, l.Status, l.LoadPay, l.TotalOtherPay, l.TotalPay,
			l.TotalMiles, l.PerMileRevenue, l.DispatcherName, l.DriverName, l.TeamDriverName, l.TruckUnit, l.CustomerName,
			l.PickupTime, l.DeliveryTime, l.PickupAppointmentTime, l.DeliveryAppointmentTime, l.CreatedDatetime, l.SyncedAt, l.RawPayload)
	}
	results := tx.SendBatch(ctx, batch)
	for range records {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
