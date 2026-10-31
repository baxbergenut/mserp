package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

type payrollFinalizeKey struct{}
type payrollFinalizeInputs struct {
	sources []paySourceRecord
	costs   []payCostRecord
	closed  map[string]bool
}

// Under the exclusive financial lock, distinct principals can be validated
// together before any collection writes. Shared principals retain sequential
// validation. This is a write-plan snapshot, never a general read cache.
func preparePayrollFinalization(ctx context.Context, tx pgx.Tx, kind string, report DriverPayWeek) (context.Context, error) {
	seen := map[string]bool{}
	type check struct {
		Source string `json:"source"`
		Truck  string `json:"truck"`
	}
	checks := []check{}
	for _, d := range report.Drivers {
		if d.Settlement != nil && d.Settlement.Finalized || kind == "investor" && d.TruckInactive {
			continue
		}
		for _, row := range d.Edits.SourceCharges {
			key := sourceKey(row.Source, row.OwnerID)
			if seen[key] {
				return ctx, nil
			}
			seen[key] = true
			truck := d.TruckID
			if strings.HasPrefix(row.Source, "truck_charge:") || strings.HasPrefix(row.Source, "investor_cost:") {
				truck = strings.Split(row.Source, ":")[1]
			}
			checks = append(checks, check{row.Source, truck})
		}
	}
	ctx = payrollWeekContext(ctx, report.WeekStart)
	inputs := &payrollFinalizeInputs{closed: map[string]bool{}}
	var err error
	if len(checks) > 0 {
		inputs.sources, err = readPaySources(ctx, tx)
		if err != nil {
			return ctx, err
		}
		body, _ := json.Marshal(checks)
		rows, e := tx.Query(ctx, `WITH requested AS (SELECT DISTINCT source,truck FROM jsonb_to_recordset($1::jsonb) r(source text,truck uuid))
 SELECT r.source,r.truck::text,EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=r.truck AND w.week_start>=$2::date AND w.finalized)
 OR EXISTS(SELECT 1 FROM payroll_settlements p WHERE p.week_start>=$2::date AND p.finalized AND EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(p.report->'autoCharges','[]')) a WHERE a->>'source'=r.source)) FROM requested r`, body, report.WeekStart)
		if e != nil {
			return ctx, e
		}
		for rows.Next() {
			var source, truck string
			var closed bool
			if err = rows.Scan(&source, &truck, &closed); err != nil {
				rows.Close()
				return ctx, err
			}
			inputs.closed[source+"|"+truck] = closed
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ctx, err
		}
	}
	if kind == "driver" {
		inputs.costs, err = readPayCostRecords(ctx, tx)
		if err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, payrollFinalizeKey{}, inputs), nil
}
