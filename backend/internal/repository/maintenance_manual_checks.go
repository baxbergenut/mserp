package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func checkManualOdometer(ctx context.Context, q chargeQuery, in MaintenanceReading) error {
	zone, _ := time.LoadLocation("America/New_York")
	if in.Mileage < 0 || in.Mileage > 2000000000 || utf8.RuneCountInString(in.Note) > 500 || in.Date > time.Now().In(zone).Format(time.DateOnly) {
		return ErrMaintenanceInvalid
	}
	if _, err := time.Parse(time.DateOnly, in.Date); err != nil {
		return ErrMaintenanceInvalid
	}
	if in.ObservedAt != nil && (in.ObservedAt.After(time.Now()) || in.ObservedAt.In(zone).Format(time.DateOnly) != in.Date) {
		return fmt.Errorf("%w: observation time must match the reading date and cannot be in the future", ErrMaintenanceInvalid)
	}
	// Exclude this exact manual revision when correcting a typo. The reviewed
	// service history and other manual observations continue to constrain it.
	withoutSelf := func(s string) string {
		return strings.ReplaceAll(s, "AND o.source='manual'", "AND o.source='manual' AND o.id IS DISTINCT FROM NULLIF($5,'')::uuid")
	}
	check := `candidate.reported_at<=now() AND NOT EXISTS(SELECT 1 FROM (` + withoutSelf(maintenanceOdometerAnchorsSQL) + `) anchor WHERE
 (anchor.reading_date<candidate.reading_date AND anchor.mileage>candidate.mileage)
 OR (anchor.reading_date>candidate.reading_date AND anchor.mileage<candidate.mileage)
 OR abs(anchor.mileage::bigint-candidate.mileage)>100+2400::bigint*(abs(anchor.reading_date-candidate.reading_date)+1))`
	var valid bool
	err := q.QueryRow(ctx, `SELECT `+check+` FROM trucks t CROSS JOIN LATERAL (
 SELECT $2::integer AS mileage,$3::date AS reading_date,coalesce($4::timestamptz,$3::date::timestamp AT TIME ZONE 'America/New_York') AS reported_at
 ) candidate WHERE t.id=$1`, in.TruckID, in.Mileage, in.Date, in.ObservedAt, in.ID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: manual mileage conflicts with reviewed history or makes an implausible jump", ErrMaintenanceInvalid)
	}
	// A new current observation that disagrees sharply with valid telemetry is
	// held for correction/review instead of becoming a trusted blocking anchor.
	err = q.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM (`+withoutSelf(maintenanceOdometerCandidatesSQL)+`) c
 WHERE c.reported_at>=now()-interval '1 hour' AND
 abs(c.mileage::bigint-$2::integer)>CASE WHEN $4::timestamptz IS NOT NULL THEN 25+100*abs(extract(epoch FROM(c.reported_at-$4::timestamptz)))/3600
 ELSE 100+2400::bigint*(abs(c.reading_date-$3::date)+1) END)
 FROM trucks t WHERE t.id=$1`, in.TruckID, in.Mileage, in.Date, in.ObservedAt, in.ID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: manual reading differs substantially from valid provider mileage; review the source before saving", ErrMaintenanceInvalid)
	}
	return nil
}
