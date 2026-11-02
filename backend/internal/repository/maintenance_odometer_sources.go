package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type OdometerSourceReading struct {
	Source     string     `json:"source"`
	Mileage    *int       `json:"mileage"`
	ObservedAt *time.Time `json:"observedAt"`
	Date       *string    `json:"date"`
	Valid      bool       `json:"valid"`
	Problem    string     `json:"problem"`
}
type OdometerSources struct {
	TruckID    string                  `json:"truckId"`
	UnitNumber string                  `json:"unitNumber"`
	Source     string                  `json:"source"`
	Version    int                     `json:"version"`
	Current    *OdometerSourceReading  `json:"current"`
	Readings   []OdometerSourceReading `json:"readings"`
	Manual     *MaintenanceReading     `json:"manual"`
}
type OdometerSourceInput struct {
	Source    string `json:"source"`
	Version   int    `json:"version"`
	ActorID   string `json:"-"`
	ActorName string `json:"-"`
}

func (r *MaintenanceRepository) OdometerSources(ctx context.Context, truck string) (OdometerSources, error) {
	return odometerSources(ctx, r.pool, truck)
}

func odometerSources(ctx context.Context, q chargeQuery, truck string) (OdometerSources, error) {
	var out OdometerSources
	var current OdometerSourceReading
	var currentSource *string
	err := q.QueryRow(ctx, `SELECT t.id,t.unit_number,coalesce(choice.source,'auto'),coalesce(choice.version,0),r.mileage,r.reported_at,r.reading_date::text,r.source FROM trucks t
 LEFT JOIN maintenance_odometer_sources choice ON choice.truck_id=t.id `+maintenanceCurrentReadingJoin+` WHERE t.id=$1`, truck).Scan(&out.TruckID, &out.UnitNumber, &out.Source, &out.Version, &current.Mileage, &current.ObservedAt, &current.Date, &currentSource)
	if err != nil {
		return out, mapNotFound(err)
	}
	if current.Mileage != nil {
		current.Source = pmRecordID(currentSource)
		current.Valid = true
		out.Current = &current
	}
	out.Readings = []OdometerSourceReading{}
	for _, source := range []string{"samsara", "five_eld"} {
		v := OdometerSourceReading{Source: source, Problem: "No matched provider odometer is available"}
		var providerID *string
		err = q.QueryRow(ctx, `SELECT c.mileage,c.reported_at,(c.reported_at AT TIME ZONE 'America/New_York')::date::text,c.provider_id FROM trucks t LEFT JOIN LATERAL (
 SELECT coalesce(s.observed_mileage,s.mileage) AS mileage,coalesce(s.observed_at,s.odometer_at) AS reported_at,s.vehicle_id AS provider_id
 FROM samsara_telemetry s WHERE $2='samsara' AND s.available AND s.vin=upper(btrim(t.vin))
 UNION ALL SELECT coalesce(f.observed_mileage,f.mileage),coalesce(f.observed_at,f.reported_at),'' FROM maintenance_telemetry f WHERE $2='five_eld' AND f.truck_id=t.id AND f.vin=upper(btrim(t.vin))
 ) c ON (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=upper(btrim(t.vin)))=1 WHERE t.id=$1`, truck, source).Scan(&v.Mileage, &v.ObservedAt, &v.Date, &providerID)
		if err != nil {
			return out, err
		}
		if v.Mileage != nil && v.ObservedAt != nil {
			v.Valid, err = checkMaintenanceTelemetry(ctx, q, truck, source, pmRecordID(providerID), MaintenanceTelemetry{Mileage: *v.Mileage, ReportedAt: *v.ObservedAt}, true)
			if err != nil {
				return out, err
			}
			if v.Valid {
				v.Problem = ""
			} else {
				v.Problem = "Conflicts with saved service/manual mileage or makes an implausible jump"
			}
		}
		out.Readings = append(out.Readings, v)
	}
	var manual MaintenanceReading
	err = q.QueryRow(ctx, `SELECT id,truck_id,record_id,reading_date::text,mileage,source,version,actor_name,updated_at,observed_at,note FROM maintenance_odometer_readings WHERE truck_id=$1 AND active AND source='manual' ORDER BY reading_date DESC,updated_at DESC,id DESC LIMIT 1`, truck).Scan(&manual.ID, &manual.TruckID, &manual.RecordID, &manual.Date, &manual.Mileage, &manual.Source, &manual.Version, &manual.ActorName, &manual.UpdatedAt, &manual.ObservedAt, &manual.Note)
	v := OdometerSourceReading{Source: "manual", Problem: "No manual observation has been recorded"}
	if err != nil && err != pgx.ErrNoRows {
		return out, err
	}
	if err == nil {
		out.Manual = &manual
		v.Mileage = &manual.Mileage
		v.Date = &manual.Date
		v.ObservedAt = manual.ObservedAt
		if err = checkManualOdometer(ctx, q, manual); err == nil {
			v.Valid = true
			v.Problem = ""
		} else if errors.Is(err, ErrMaintenanceInvalid) {
			v.Problem = "Manual reading needs correction before use"
		} else {
			return out, err
		}
	}
	out.Readings = append(out.Readings, v)
	return out, nil
}

func (r *MaintenanceRepository) SaveOdometerSource(ctx context.Context, truck string, in OdometerSourceInput) error {
	if in.Source != "auto" && in.Source != "samsara" && in.Source != "five_eld" && in.Source != "manual" || in.Version < 0 {
		return ErrMaintenanceInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM trucks WHERE id=$1 FOR UPDATE`, truck).Scan(&id); err != nil {
		return mapNotFound(err)
	}
	before, err := odometerSources(ctx, tx, truck)
	if err != nil {
		return err
	}
	if before.Version != in.Version {
		return ErrMaintenanceConflict
	}
	if in.Source != "auto" {
		valid := false
		for _, v := range before.Readings {
			if v.Source == in.Source && v.Valid {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("%w: the selected source has no usable reading; review its mileage and timestamp", ErrMaintenanceInvalid)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO maintenance_odometer_sources(truck_id,source,actor_name) VALUES($1,$2,$3)
 ON CONFLICT(truck_id) DO UPDATE SET source=excluded.source,actor_name=excluded.actor_name,version=maintenance_odometer_sources.version+1,updated_at=now()`, truck, in.Source, in.ActorName)
	if err != nil {
		return err
	}
	// Selecting a provider can accept its latest raw point after the same history
	// and continuity checks. A deliberate Five ELD choice may resolve a disagreement
	// with Samsara; it never bypasses the recorded truck mileage history.
	if in.Source == "samsara" || in.Source == "five_eld" {
		for _, v := range before.Readings {
			if v.Source == in.Source {
				if in.Source == "samsara" {
					_, err = tx.Exec(ctx, `UPDATE samsara_telemetry SET mileage=$2,odometer_at=$3 WHERE available AND vin=(SELECT upper(btrim(vin)) FROM trucks WHERE id=$1) AND (odometer_at IS NULL OR odometer_at<=$3)`, truck, v.Mileage, v.ObservedAt)
				} else {
					_, err = tx.Exec(ctx, `UPDATE maintenance_telemetry SET mileage=$2,reported_at=$3 WHERE truck_id=$1 AND vin=(SELECT upper(btrim(vin)) FROM trucks WHERE id=$1) AND (reported_at IS NULL OR reported_at<=$3)`, truck, v.Mileage, v.ObservedAt)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	if err = accessAudit(ctx, tx, in.ActorID, "maintenance_odometer_source", truck, map[string]any{"before": before.Source, "after": in.Source, "actorName": in.ActorName}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
