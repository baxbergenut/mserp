package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type MaintenanceTelemetryTruck struct {
	TruckID string
	VIN     string
}
type MaintenanceTelemetry struct {
	Source     string    `json:"source"`
	Mileage    int       `json:"mileage"`
	ReportedAt time.Time `json:"reportedAt"`
}

// Background polling is independent of page visits and service intervals. Check
// the current exact unique VIN again on write and read, including after edits.
func (r *MaintenanceRepository) MaintenanceTelemetryCandidates(ctx context.Context) ([]MaintenanceTelemetryTruck, error) {
	rows, err := r.pool.Query(ctx, `SELECT t.id,upper(btrim(t.vin)) FROM trucks t LEFT JOIN maintenance_telemetry c ON c.truck_id=t.id AND c.vin=upper(btrim(t.vin)) WHERE t.active AND length(btrim(t.vin))=17 AND (c.checked_at IS NULL OR c.checked_at<now()-interval '5 minutes') AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=upper(btrim(t.vin)))=1 ORDER BY c.checked_at NULLS FIRST,t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaintenanceTelemetryTruck{}
	for rows.Next() {
		var v MaintenanceTelemetryTruck
		if err := rows.Scan(&v.TruckID, &v.VIN); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *MaintenanceRepository) StoreMaintenanceTelemetry(ctx context.Context, truck MaintenanceTelemetryTruck, reading *MaintenanceTelemetry) error {
	observed := reading
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM trucks WHERE id=$1 AND active AND upper(btrim(vin))=$2 AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=$2)=1 FOR UPDATE`, truck.TruckID, truck.VIN).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	if reading != nil {
		ok, err := acceptMaintenanceTelemetry(ctx, tx, truck.TruckID, "five_eld", "", *reading)
		if err != nil {
			return err
		}
		if !ok {
			reading = nil
		}
	}
	var miles *int
	var at *time.Time
	if reading != nil {
		miles = &reading.Mileage
		at = &reading.ReportedAt
	}
	_, err = tx.Exec(ctx, `INSERT INTO maintenance_telemetry(truck_id,vin,mileage,reported_at) SELECT t.id,$2,$3,$4 FROM trucks t WHERE t.id=$1 AND t.active AND upper(btrim(t.vin))=$2 AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=$2)=1
 ON CONFLICT(truck_id) DO UPDATE SET vin=excluded.vin,checked_at=now(),observed_mileage=CASE WHEN maintenance_telemetry.vin<>excluded.vin THEN NULL ELSE maintenance_telemetry.observed_mileage END,observed_at=CASE WHEN maintenance_telemetry.vin<>excluded.vin THEN NULL ELSE maintenance_telemetry.observed_at END,mileage=CASE WHEN maintenance_telemetry.vin<>excluded.vin THEN excluded.mileage WHEN excluded.reported_at>=maintenance_telemetry.reported_at OR maintenance_telemetry.reported_at IS NULL THEN excluded.mileage ELSE maintenance_telemetry.mileage END,reported_at=CASE WHEN maintenance_telemetry.vin<>excluded.vin THEN excluded.reported_at ELSE coalesce(greatest(maintenance_telemetry.reported_at,excluded.reported_at),maintenance_telemetry.reported_at) END`, truck.TruckID, truck.VIN, miles, at)
	if err != nil {
		return err
	}
	if observed != nil {
		_, err = tx.Exec(ctx, `UPDATE maintenance_telemetry SET observed_mileage=$2,observed_at=$3 WHERE truck_id=$1 AND vin=$4 AND (observed_at IS NULL OR observed_at<=$3)`, truck.TruckID, observed.Mileage, observed.ReportedAt, truck.VIN)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *MaintenanceRepository) CachedMaintenanceOdometer(ctx context.Context, truck string) (*MaintenanceTelemetry, error) {
	var value MaintenanceTelemetry
	err := r.pool.QueryRow(ctx, `SELECT c.mileage,c.reported_at,c.source FROM trucks t
 LEFT JOIN maintenance_odometer_sources choice ON choice.truck_id=t.id
 CROSS JOIN LATERAL (`+maintenanceOdometerCandidatesSQL+`
 UNION ALL SELECT o.mileage,o.observed_at,o.reading_date,'manual' FROM (`+maintenanceManualOdometerSQL+`) o WHERE choice.source='manual') c
 WHERE t.id=$1 AND c.reported_at>=now()-interval '1 hour' AND (coalesce(choice.source,'auto')='auto' OR c.source=choice.source)
 ORDER BY CASE c.source WHEN 'samsara' THEN 0 ELSE 1 END,c.reported_at DESC LIMIT 1`, truck).Scan(&value.Mileage, &value.ReportedAt, &value.Source)
	if err != nil {
		if mapNotFound(err) == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &value, nil
}

var maintenanceManualOdometerSQL = `SELECT o.* FROM maintenance_odometer_readings o CROSS JOIN LATERAL (
 SELECT o.mileage,o.reading_date,coalesce(o.observed_at,o.reading_date::timestamp AT TIME ZONE 'America/New_York') AS reported_at
 ) candidate WHERE o.truck_id=t.id AND o.active AND o.source='manual' AND ` + maintenanceOdometerPlausibleSQL("candidate")

var maintenanceCurrentReadingJoin = ` LEFT JOIN LATERAL (
 SELECT mileage,reading_date,reported_at,source FROM (
 SELECT o.mileage,o.reading_date,NULL::timestamptz AS reported_at,o.source,2 AS priority FROM (` + maintenanceOdometerAnchorsSQL + `) o WHERE o.source<>'manual'
 UNION ALL SELECT o.mileage,o.reading_date,o.observed_at,'manual',2 FROM (` + maintenanceManualOdometerSQL + `) o
 UNION ALL SELECT c.mileage,c.reading_date,c.reported_at,c.source,
 CASE WHEN c.reported_at<now()-interval '1 hour' THEN 2 WHEN c.source='samsara' THEN 0 ELSE 1 END
 FROM (` + maintenanceOdometerCandidatesSQL + `) c
 ) observed WHERE coalesce((SELECT source FROM maintenance_odometer_sources WHERE truck_id=t.id),'auto') IN ('auto',observed.source)
 ORDER BY priority,reading_date DESC,mileage DESC,reported_at DESC NULLS LAST LIMIT 1
 ) r ON true `
