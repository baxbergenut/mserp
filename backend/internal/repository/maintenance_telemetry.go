package repository

import (
	"context"
	"time"
)

type MaintenanceTelemetryTruck struct {
	TruckID string
	VIN     string
}
type MaintenanceTelemetry struct {
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
	var miles *int
	var at *time.Time
	if reading != nil {
		miles = &reading.Mileage
		at = &reading.ReportedAt
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO maintenance_telemetry(truck_id,vin,mileage,reported_at) SELECT t.id,$2,$3,$4 FROM trucks t WHERE t.id=$1 AND t.active AND upper(btrim(t.vin))=$2 AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=$2)=1
 ON CONFLICT(truck_id) DO UPDATE SET vin=excluded.vin,checked_at=now(),mileage=CASE WHEN maintenance_telemetry.vin<>excluded.vin THEN excluded.mileage WHEN excluded.reported_at>=maintenance_telemetry.reported_at OR maintenance_telemetry.reported_at IS NULL THEN excluded.mileage ELSE maintenance_telemetry.mileage END,reported_at=CASE WHEN maintenance_telemetry.vin<>excluded.vin THEN excluded.reported_at ELSE coalesce(greatest(maintenance_telemetry.reported_at,excluded.reported_at),maintenance_telemetry.reported_at) END`, truck.TruckID, truck.VIN, miles, at)
	return err
}
func (r *MaintenanceRepository) CachedMaintenanceOdometer(ctx context.Context, truck string) (*MaintenanceTelemetry, error) {
	var value MaintenanceTelemetry
	err := r.pool.QueryRow(ctx, `SELECT c.mileage,c.reported_at FROM maintenance_telemetry c JOIN trucks t ON t.id=c.truck_id AND upper(btrim(t.vin))=c.vin WHERE t.id=$1 AND c.reported_at<=now() AND c.reported_at>=now()-interval '1 hour' AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=c.vin)=1`, truck).Scan(&value.Mileage, &value.ReportedAt)
	if err != nil {
		if mapNotFound(err) == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &value, nil
}

const maintenanceCurrentReadingJoin = ` LEFT JOIN LATERAL (
 SELECT mileage,reading_date,reported_at,source FROM (
 SELECT o.mileage,o.reading_date,NULL::timestamptz AS reported_at,o.source FROM maintenance_odometer_readings o WHERE o.truck_id=t.id AND o.active
 UNION ALL SELECT c.mileage,(c.reported_at AT TIME ZONE 'America/New_York')::date,c.reported_at,'five_eld' FROM maintenance_telemetry c WHERE c.truck_id=t.id AND c.vin=upper(btrim(t.vin)) AND c.reported_at<=now() AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=c.vin)=1
 ) observed ORDER BY reading_date DESC,mileage DESC,reported_at DESC NULLS LAST LIMIT 1
 ) r ON true `
