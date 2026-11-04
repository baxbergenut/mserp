package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Frozen financial cards retain their amounts and identity. Refresh only their
// comparison evidence, in one bounded read for the displayed cards. No calculator
// runs and no settlement JSON or financial revision is written.
func decorateFrozenSourceComparisons(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	type identity struct {
		Load int  `json:"load_id"`
		Trip *int `json:"load_trip_id"`
	}
	ids := []identity{}
	seen := map[string]bool{}
	for _, d := range report.Drivers {
		if d.Settlement == nil || !d.Settlement.Finalized {
			continue
		}
		for _, l := range d.Loads {
			if l.LoadRecordID == nil {
				continue
			}
			key := loadAllocationKey(*l.LoadRecordID, l.LoadTripID)
			if !seen[key] {
				seen[key] = true
				ids = append(ids, identity{*l.LoadRecordID, l.LoadTripID})
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT e.load_id,e.load_trip_id,coalesce(trip_source.original::text,''),coalesce(trip_source.driver_gross::text,''),coalesce(trip_source.miles::text,'')
 FROM jsonb_to_recordset($1::jsonb) e(load_id integer,load_trip_id integer)
 LEFT JOIN loads l ON l.id=e.load_id `+loadTripSourceSQL, encoded)
	if err != nil {
		return err
	}
	defer rows.Close()
	values := map[string][3]string{}
	for rows.Next() {
		var id identity
		var v [3]string
		if err = rows.Scan(&id.Load, &id.Trip, &v[0], &v[1], &v[2]); err != nil {
			return err
		}
		values[loadAllocationKey(id.Load, id.Trip)] = v
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement == nil || !d.Settlement.Finalized {
			continue
		}
		for j := range d.Loads {
			l := &d.Loads[j]
			if l.LoadRecordID == nil {
				continue
			}
			v := values[loadAllocationKey(*l.LoadRecordID, l.LoadTripID)]
			l.SystemOriginalRate, l.SystemDriverGross, l.SystemMiles = v[0], v[1], v[2]
		}
	}
	return nil
}
