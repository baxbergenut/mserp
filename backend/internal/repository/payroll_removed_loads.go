package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Removed rows are display-only and never enter Loads, totals or review flags.
func decorateRemovedPayLoads(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	ids := []string{}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement == nil || !d.Settlement.Finalized {
			d.RemovedLoads = nil
			if d.Summary == nil {
				ids = append(ids, d.ID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	destination := `payroll_driver_at((h.after_value->>'driver_id')::uuid,$1::date)::text`
	if kind == "investor" {
		destination = `h.after_value->>'truck_id'`
	}
	rows, err := tx.Query(ctx, `WITH decisions AS (
 SELECT DISTINCT ON (after_value->>'load_record_id',after_value->>'load_trip_id') *
 FROM payroll_load_placement_history WHERE action IN ('remove','restore') AND after_value->>'week_start'=$1
 ORDER BY after_value->>'load_record_id',after_value->>'load_trip_id',id DESC)
 SELECT `+destination+`,h.after_value,h.before_value,p.version,l.load_id,coalesce(h.after_value->>'driver_gross',''),coalesce(h.after_value->>'original_rate',''),coalesce(h.after_value->>'miles','')
 FROM decisions h JOIN payroll_load_placements p ON p.load_record_id=(h.after_value->>'load_record_id')::integer AND p.load_trip_id=(h.after_value->>'load_trip_id')::integer
 JOIN loads l ON l.id=p.load_record_id WHERE h.action='remove' AND NOT (p.included AND p.week_start=$1::date) AND `+destination+`=ANY($2::text[])
 ORDER BY h.id`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string][]DriverPayLoad{}
	for rows.Next() {
		var id, number, gross, original, miles string
		var after, before []byte
		var version int
		if err = rows.Scan(&id, &after, &before, &version, &number, &gross, &original, &miles); err != nil {
			return err
		}
		var metadata struct {
			Removed    *DriverPayLoad `json:"removed_load"`
			Record     int            `json:"load_record_id"`
			Trip       int            `json:"load_trip_id"`
			Driver     string         `json:"driver_id"`
			SourceDate string         `json:"source_date"`
			SourceSlot int            `json:"source_slot"`
			Slot       int            `json:"slot"`
		}
		if err = json.Unmarshal(after, &metadata); err != nil {
			return err
		}
		var load DriverPayLoad
		if metadata.Removed != nil {
			load = *metadata.Removed
		} else {
			var previous struct {
				Included bool `json:"included"`
			}
			_ = json.Unmarshal(before, &previous)
			load = DriverPayLoad{LoadRecordID: &metadata.Record, LoadNumber: number, SourceDriverID: metadata.Driver, SourceDate: metadata.SourceDate, SourceSlot: metadata.SourceSlot, Slot: metadata.Slot, Date: metadata.SourceDate, PayrollSupplement: previous.Included, DriverGross: gross, OriginalRate: original, TotalMiles: miles}
			if metadata.Trip > 0 {
				load.LoadTripID = &metadata.Trip
			}
			if load.PayrollSupplement {
				w, e := time.Parse(time.DateOnly, report.WeekStart)
				if e != nil {
					return e
				}
				load.Date = w.AddDate(0, 0, 6).Format(time.DateOnly)
			}
		}
		load.SourceDriverID = metadata.Driver
		load.CommentKey = fmt.Sprintf("removed:%d:%d:%s", metadata.Record, metadata.Trip, report.WeekStart)
		load.PayrollPlacementVersion = version
		byID[id] = append(byID[id], load)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement == nil || !d.Settlement.Finalized {
			d.RemovedLoads = byID[d.ID]
		}
	}
	return nil
}
