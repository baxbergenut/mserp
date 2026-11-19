package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fiveeld"
	"mserp/internal/samsara"
)

type SamsaraRepository struct{ pool *pgxpool.Pool }

func NewSamsaraRepository(pool *pgxpool.Pool) *SamsaraRepository { return &SamsaraRepository{pool} }

func (r *SamsaraRepository) MileageEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_programs p
 LEFT JOIN maintenance_schedules s ON p.legacy_pm AND s.truck_id IS NULL AND s.service_code='pm'
 WHERE p.active AND coalesce(s.enabled,true))`).Scan(&enabled)
	return enabled, err
}

func (r *SamsaraRepository) Store(ctx context.Context, s samsara.Snapshot, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// One complete roster transaction: removed/ambiguous identities stop being
	// eligible, while a failed provider request leaves the previous cache intact.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(718504132)`); err != nil {
		return err
	}
	var newer bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM samsara_sync_state WHERE singleton AND last_success_at>$1)`, now).Scan(&newer); err != nil {
		return err
	}
	if newer {
		return nil
	}
	type reading struct {
		VIN        string     `json:"vin"`
		VehicleID  string     `json:"vehicle_id"`
		Unit       string     `json:"unit_number"`
		Lat        *float64   `json:"latitude"`
		Lng        *float64   `json:"longitude"`
		Heading    *float64   `json:"heading"`
		Speed      *float64   `json:"speed_mph"`
		GPSAt      *time.Time `json:"gps_at"`
		Mileage    *int       `json:"mileage"`
		OdometerAt *time.Time `json:"odometer_at"`
		Observed   *int       `json:"observed_mileage"`
		ObservedAt *time.Time `json:"observed_at"`
	}
	readings := make([]reading, 0, len(s.Vehicles))
	for _, v := range s.Vehicles {
		row := reading{VIN: v.VIN, VehicleID: v.ID, Unit: v.Name}
		if v.GPS != nil {
			row.Lat, row.Lng, row.Heading, row.Speed, row.GPSAt = &v.GPS.Latitude, &v.GPS.Longitude, v.GPS.Heading, v.GPS.SpeedMPH, &v.GPS.Time
		}
		if v.Odometer != nil {
			row.Observed, row.ObservedAt = &v.Odometer.Miles, &v.Odometer.Time
			var truck string
			if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT id::text FROM trucks WHERE upper(btrim(vin))=$1 AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=$1)=1),'')`, v.VIN).Scan(&truck); err != nil {
				return err
			}
			ok := true
			if truck != "" {
				ok, err = acceptMaintenanceTelemetry(ctx, tx, truck, "samsara", v.ID, MaintenanceTelemetry{Mileage: v.Odometer.Miles, ReportedAt: v.Odometer.Time})
				if err != nil {
					return err
				}
			}
			if ok {
				row.Mileage = &v.Odometer.Miles
				row.OdometerAt = &v.Odometer.Time
			}
		}
		readings = append(readings, row)
	}
	raw, err := json.Marshal(readings)
	if err != nil {
		return err
	}
	const incoming = `jsonb_to_recordset($1::jsonb) AS v(vin text,vehicle_id text,unit_number text,latitude float8,longitude float8,heading float8,speed_mph float8,gps_at timestamptz,mileage integer,odometer_at timestamptz,observed_mileage integer,observed_at timestamptz)`
	var batch pgx.Batch
	batch.Queue(`UPDATE samsara_telemetry t SET available=false WHERE t.available AND NOT EXISTS (SELECT 1 FROM `+incoming+` WHERE v.vin=t.vin)`, raw)
	batch.Queue(`DELETE FROM samsara_tracking h USING samsara_telemetry t,`+incoming+` WHERE h.vin=t.vin AND t.vin=v.vin AND t.vehicle_id<>v.vehicle_id`, raw)
	batch.Queue(`INSERT INTO samsara_telemetry(vin,vehicle_id,unit_number,latitude,longitude,heading,speed_mph,gps_at,mileage,odometer_at,fetched_at,observed_mileage,observed_at)
 SELECT vin,vehicle_id,unit_number,latitude,longitude,heading,speed_mph,gps_at,mileage,odometer_at,$2,observed_mileage,observed_at FROM `+incoming+` WHERE true
 ON CONFLICT(vin) DO UPDATE SET vehicle_id=excluded.vehicle_id,unit_number=excluded.unit_number,available=true,fetched_at=excluded.fetched_at,
 observed_mileage=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.observed_at IS NULL OR excluded.observed_at >= samsara_telemetry.observed_at THEN excluded.observed_mileage ELSE samsara_telemetry.observed_mileage END,
 observed_at=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.observed_at IS NULL OR excluded.observed_at >= samsara_telemetry.observed_at THEN excluded.observed_at ELSE samsara_telemetry.observed_at END,
 latitude=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.latitude ELSE samsara_telemetry.latitude END,
 longitude=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.longitude ELSE samsara_telemetry.longitude END,
 heading=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.heading ELSE samsara_telemetry.heading END,
 speed_mph=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.speed_mph ELSE samsara_telemetry.speed_mph END,
 gps_at=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.gps_at ELSE samsara_telemetry.gps_at END,
 mileage=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.odometer_at IS NULL OR excluded.odometer_at>=samsara_telemetry.odometer_at THEN excluded.mileage ELSE samsara_telemetry.mileage END,
 odometer_at=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.odometer_at IS NULL OR excluded.odometer_at>=samsara_telemetry.odometer_at THEN excluded.odometer_at ELSE samsara_telemetry.odometer_at END`, raw, now)
	batch.Queue(`INSERT INTO samsara_tracking(vin,reported_at,latitude,longitude,speed_mph)
 SELECT vin,gps_at,latitude,longitude,speed_mph FROM `+incoming+` WHERE gps_at IS NOT NULL ON CONFLICT(vin,reported_at) DO NOTHING`, raw)
	if err = tx.SendBatch(ctx, &batch).Close(); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM samsara_tracking WHERE reported_at<$1::timestamptz-interval '1 hour'`, now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO samsara_sync_state(singleton,last_attempt_at,last_success_at,fetched,saved,ambiguous_vins,invalid_count) VALUES(true,$1,$1,$2,$3,$4,$5) ON CONFLICT(singleton) DO UPDATE SET last_attempt_at=excluded.last_attempt_at,last_success_at=excluded.last_success_at,last_error='',fetched=excluded.fetched,saved=excluded.saved,ambiguous_vins=excluded.ambiguous_vins,invalid_count=excluded.invalid_count`, now, s.Fetched, len(s.Vehicles), s.Ambiguous, s.Invalid)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *SamsaraRepository) RecordFailure(ctx context.Context, now time.Time, err error) error {
	_, e := r.pool.Exec(ctx, `INSERT INTO samsara_sync_state(singleton,last_attempt_at,last_error) VALUES(true,$1,$2) ON CONFLICT(singleton) DO UPDATE SET last_attempt_at=excluded.last_attempt_at,last_error=excluded.last_error`, now, err.Error())
	return e
}

func (r *DriverBoardRepository) samsaraTracking(ctx context.Context, vin string, now time.Time) ([]fiveeld.TrackingPoint, error) {
	rows, err := r.pool.Query(ctx, `SELECT h.latitude,h.longitude,h.speed_mph,h.reported_at FROM samsara_tracking h JOIN samsara_telemetry c ON c.vin=h.vin AND c.available WHERE h.vin=upper(btrim($1)) AND h.reported_at BETWEEN $2::timestamptz-interval '10 minutes' AND $2 ORDER BY h.reported_at`, vin, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []fiveeld.TrackingPoint{}
	for rows.Next() {
		var p fiveeld.TrackingPoint
		if err = rows.Scan(&p.Coordinates.Lat, &p.Coordinates.Lng, &p.Speed, &p.Date); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}
