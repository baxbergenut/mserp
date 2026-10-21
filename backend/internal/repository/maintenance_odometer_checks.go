package repository

import (
	"context"
	"math"
	"time"
)

// Dated manual/service values are the reviewed mileage history. A current
// program baseline supersedes its exact source work-order revision; do not
// accidentally reinstate that revision's old odometer as a validation anchor.
const maintenanceOdometerAnchorsSQL = `
 SELECT o.reading_date,o.mileage,'manual'::text AS source FROM maintenance_odometer_readings o
 WHERE o.truck_id=t.id AND o.active AND o.source='manual'
 UNION ALL
 SELECT v.service_date,v.odometer,'service'::text FROM maintenance_records v
 WHERE v.truck_id=t.id AND v.status='completed' AND v.service_date IS NOT NULL AND v.odometer IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM maintenance_pm_baselines b WHERE b.truck_id=t.id AND b.source_record_id=v.id AND b.source_version=v.version)
 AND NOT EXISTS(SELECT 1 FROM service_program_trucks b WHERE b.truck_id=t.id AND b.baseline_date IS NOT NULL AND b.baseline_source_id=v.id AND b.baseline_source_version=v.version)
 UNION ALL
 SELECT m.service_date,m.odometer,'service'::text FROM service_programs p
 LEFT JOIN service_program_trucks member ON member.program_id=p.id AND member.truck_id=t.id
 ` + programCycleJoin + `
 WHERE m.service_date IS NOT NULL AND m.odometer IS NOT NULL AND (p.legacy_pm OR member.truck_id IS NOT NULL)
`

// A telemetry reading cannot precede reviewed history, go backwards, or imply
// more than 100 mph continuously. Dates have no time-of-day, so allow the whole
// observation day plus 100 miles of measurement tolerance. Precise provider
// samples below use elapsed hours plus 25 miles instead.
func maintenanceOdometerPlausibleSQL(alias string) string {
	return alias + `.mileage BETWEEN 0 AND 2000000000 AND ` + alias + `.reported_at<=now() AND NOT EXISTS (
 SELECT 1 FROM (` + maintenanceOdometerAnchorsSQL + `) anchor
 WHERE anchor.reading_date>` + alias + `.reading_date
 OR anchor.mileage>` + alias + `.mileage
 OR ` + alias + `.mileage::bigint-anchor.mileage>100+2400::bigint*(` + alias + `.reading_date-anchor.reading_date+1))`
}

const maintenanceRawSamsaraOdometerSQL = `SELECT s.mileage,s.odometer_at AS reported_at,
 (s.odometer_at AT TIME ZONE 'America/New_York')::date AS reading_date,'samsara'::text AS source
 FROM samsara_telemetry s WHERE s.available AND s.vin=upper(btrim(t.vin)) AND s.odometer_at IS NOT NULL
 AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=s.vin)=1`

// Five ELD must also agree with an otherwise valid Samsara observation. This
// catches provider offsets/rollovers even on a truck with no saved service yet.
func maintenanceFiveELDPlausibleSQL(alias string) string {
	return maintenanceOdometerPlausibleSQL(alias) + ` AND NOT EXISTS (
 SELECT 1 FROM (` + maintenanceRawSamsaraOdometerSQL + `) sam WHERE ` + maintenanceOdometerPlausibleSQL("sam") + ` AND (
 (` + alias + `.reported_at>=sam.reported_at AND ` + alias + `.mileage<sam.mileage)
 OR abs(` + alias + `.mileage::bigint-sam.mileage)>25+100*abs(extract(epoch FROM (` + alias + `.reported_at-sam.reported_at)))/3600))`
}

// Revalidate on every read, including caches populated before these checks and
// after a user changes a baseline. Only fresh valid telemetry can outrank saved
// history. Last-known values retain their timestamps and normal stale status.
var maintenanceOdometerCandidatesSQL = `
 WITH sam AS (SELECT c.* FROM (` + maintenanceRawSamsaraOdometerSQL + `) c WHERE ` + maintenanceOdometerPlausibleSQL("c") + `),
 five AS (SELECT c.* FROM (
 SELECT f.mileage,f.reported_at,(f.reported_at AT TIME ZONE 'America/New_York')::date AS reading_date,'five_eld'::text AS source
 FROM maintenance_telemetry f WHERE f.truck_id=t.id AND f.vin=upper(btrim(t.vin)) AND f.reported_at IS NOT NULL
 AND (SELECT count(*) FROM trucks x WHERE upper(btrim(x.vin))=f.vin)=1
 ) c WHERE ` + maintenanceFiveELDPlausibleSQL("c") + `)
 SELECT mileage,reported_at,reading_date,source FROM sam
 UNION ALL SELECT mileage,reported_at,reading_date,source FROM five
`

// Preserve the last accepted provider sample when a new point rewinds mileage
// or makes an impossible jump. Legacy bad cache values do not block recovery:
// the previous sample itself must still pass the current reviewed history.
func acceptMaintenanceTelemetry(ctx context.Context, q chargeQuery, truck, source, providerID string, reading MaintenanceTelemetry) (bool, error) {
	check := maintenanceOdometerPlausibleSQL("candidate")
	if source == "five_eld" {
		check = maintenanceFiveELDPlausibleSQL("candidate")
	}
	var acceptable bool
	err := q.QueryRow(ctx, `SELECT `+check+` FROM trucks t CROSS JOIN LATERAL (
 SELECT $2::integer AS mileage,$3::timestamptz AS reported_at,($3::timestamptz AT TIME ZONE 'America/New_York')::date AS reading_date
 ) candidate WHERE t.id=$1`, truck, reading.Mileage, reading.ReportedAt).Scan(&acceptable)
	if err != nil || !acceptable {
		return false, err
	}
	var oldMiles *int
	var oldAt *time.Time
	err = q.QueryRow(ctx, `SELECT prior.mileage,prior.reported_at FROM trucks t LEFT JOIN LATERAL (
 SELECT c.* FROM (
 SELECT mileage,reported_at,(reported_at AT TIME ZONE 'America/New_York')::date AS reading_date
 FROM maintenance_telemetry WHERE $2='five_eld' AND truck_id=t.id AND vin=upper(btrim(t.vin))
 UNION ALL SELECT mileage,odometer_at,(odometer_at AT TIME ZONE 'America/New_York')::date
 FROM samsara_telemetry WHERE $2='samsara' AND vin=upper(btrim(t.vin)) AND vehicle_id=$3
 ) c WHERE c.reported_at IS NOT NULL AND `+maintenanceOdometerPlausibleSQL("c")+`
 ) prior ON true WHERE t.id=$1`, truck, source, providerID).Scan(&oldMiles, &oldAt)
	if err != nil {
		return false, err
	}
	if oldMiles != nil && oldAt != nil && !reading.ReportedAt.Before(*oldAt) {
		elapsed := reading.ReportedAt.Sub(*oldAt).Hours()
		if reading.Mileage < *oldMiles || math.Abs(float64(reading.Mileage-*oldMiles)) > 25+100*elapsed {
			return false, nil
		}
	}
	return true, nil
}
