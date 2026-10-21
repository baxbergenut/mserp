package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fiveeld"
	"mserp/internal/samsara"
)

type SamsaraRepository struct{ pool *pgxpool.Pool }

func NewSamsaraRepository(pool *pgxpool.Pool) *SamsaraRepository { return &SamsaraRepository{pool} }

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
	if _, err = tx.Exec(ctx, `UPDATE samsara_telemetry SET available=false WHERE available`); err != nil {
		return err
	}
	for _, v := range s.Vehicles {
		var lat, lng, heading, speed *float64
		var gpsAt, odoAt *time.Time
		var miles *int
		if v.GPS != nil {
			lat = &v.GPS.Latitude
			lng = &v.GPS.Longitude
			heading = v.GPS.Heading
			speed = v.GPS.SpeedMPH
			gpsAt = &v.GPS.Time
		}
		if v.Odometer != nil {
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
				miles = &v.Odometer.Miles
				odoAt = &v.Odometer.Time
			}
		}
		if _, err = tx.Exec(ctx, `DELETE FROM samsara_tracking WHERE vin=$1 AND EXISTS(SELECT 1 FROM samsara_telemetry WHERE vin=$1 AND vehicle_id<>$2)`, v.VIN, v.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO samsara_telemetry(vin,vehicle_id,unit_number,latitude,longitude,heading,speed_mph,gps_at,mileage,odometer_at,fetched_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
 ON CONFLICT(vin) DO UPDATE SET vehicle_id=excluded.vehicle_id,unit_number=excluded.unit_number,available=true,fetched_at=excluded.fetched_at,
 latitude=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.latitude ELSE samsara_telemetry.latitude END,
 longitude=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.longitude ELSE samsara_telemetry.longitude END,
 heading=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.heading ELSE samsara_telemetry.heading END,
 speed_mph=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.speed_mph ELSE samsara_telemetry.speed_mph END,
 gps_at=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.gps_at IS NULL OR excluded.gps_at>=samsara_telemetry.gps_at THEN excluded.gps_at ELSE samsara_telemetry.gps_at END,
 mileage=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.odometer_at IS NULL OR excluded.odometer_at>=samsara_telemetry.odometer_at THEN excluded.mileage ELSE samsara_telemetry.mileage END,
 odometer_at=CASE WHEN samsara_telemetry.vehicle_id<>excluded.vehicle_id OR samsara_telemetry.odometer_at IS NULL OR excluded.odometer_at>=samsara_telemetry.odometer_at THEN excluded.odometer_at ELSE samsara_telemetry.odometer_at END`, v.VIN, v.ID, v.Name, lat, lng, heading, speed, gpsAt, miles, odoAt, now)
		if err != nil {
			return err
		}
		if v.GPS != nil {
			if _, err = tx.Exec(ctx, `INSERT INTO samsara_tracking(vin,reported_at,latitude,longitude,speed_mph) VALUES($1,$2,$3,$4,$5) ON CONFLICT(vin,reported_at) DO NOTHING`, v.VIN, v.GPS.Time, v.GPS.Latitude, v.GPS.Longitude, v.GPS.SpeedMPH); err != nil {
				return err
			}
		}
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
