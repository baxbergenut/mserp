package repository

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type payrollDriverEligibility struct {
	enabled, release, scale, reimburse, charge, expense, escrow bool
	edits                                                       DriverPayEdits
}

func readPayrollDriverEligibility(ctx context.Context, tx pgx.Tx, week time.Time, driverID, date string) (map[string]payrollDriverEligibility, error) {
	publication, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	authoritative, _ := ctx.Value(payrollAuthoritativeKey{}).(bool)
	if publication != nil && publication.tx == tx && !authoritative && driverID == "" {
		if values, ok := publication.driverEligibility[date]; ok {
			result := map[string]payrollDriverEligibility{}
			ids := payrollDriverIDs(ctx)
			covered := ids != nil
			for _, id := range ids {
				value, ok := values[id]
				if !ok {
					covered = false
					break
				}
				value.edits.Comments = maps.Clone(value.edits.Comments)
				value.edits.Adjustments = slices.Clone(value.edits.Adjustments)
				value.edits.LayoverExcludedDates = slices.Clone(value.edits.LayoverExcludedDates)
				if value.edits.FuelOverride != nil {
					amount := *value.edits.FuelOverride
					value.edits.FuelOverride = &amount
				}
				if value.edits.TollOverride != nil {
					amount := *value.edits.TollOverride
					value.edits.TollOverride = &amount
				}
				result[id] = value
			}
			if covered {
				return result, nil
			}
		}
	}
	rows, err := tx.Query(ctx, payrollDriverEligibilitySQL, week, driverID, payrollDriverIDs(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	current := map[string]payrollDriverEligibility{}
	for rows.Next() {
		var id string
		var v payrollDriverEligibility
		var comments, adjustments []byte
		if err = rows.Scan(&id, &v.enabled, &v.release, &v.scale, &v.reimburse, &v.charge, &v.expense, &v.escrow,
			&v.edits.Notes, &comments, &adjustments, &v.edits.Version, &v.edits.FuelOverride, &v.edits.TollOverride, &v.edits.LayoverExcludedDates); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(comments, &v.edits.Comments); err == nil {
			err = json.Unmarshal(adjustments, &v.edits.Adjustments)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		v.edits.DriverID, v.edits.WeekStart = id, date
		current[id] = v
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return current, nil
}

var payrollDriverEligibilitySQL = `WITH pay_drivers AS (SELECT d.* FROM drivers d WHERE $3::uuid[] IS NULL OR d.id=ANY($3::uuid[])), ` + payrollEscrowCapacityCTE + payDriverEligibilitySQL + `
 SELECT d.id::text,d.pay_enabled,d.pay_release,d.pay_scale,d.pay_reimburse,d.pay_charge,d.pay_expense,d.pay_escrow,
 coalesce(w.notes,''),coalesce(w.comments,'{}'::jsonb),coalesce(w.adjustments,'[]'::jsonb),coalesce(w.version,0),
 w.fuel_override::text,w.toll_override::text,coalesce(w.layover_excluded_dates,'{}'::text[])
 FROM eligible_pay_drivers d LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=$1::date
 WHERE (nullif($2,'')::uuid IS NULL OR d.id=nullif($2,'')::uuid)`

// The balance batch already calculated exact escrow capacity after canonical
// writes. Read all remaining eligibility flags and saved edits for those same
// period/principal scopes together. No source or pre-write memo is reused.
func preparePayrollDriverEligibility(ctx context.Context, tx pgx.Tx, scopes []payrollBalanceScope) error {
	publication, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	batch, _ := ctx.Value(payrollBalanceBatchKey{}).(*payrollBalanceBatch)
	if publication == nil || publication.tx != tx || batch == nil || batch.tx != tx || len(scopes) < 2 {
		return nil
	}
	type capacity struct {
		Week      string `json:"week"`
		Driver    string `json:"driver"`
		ID        string `json:"id"`
		Available string `json:"available"`
	}
	capacities := []capacity{}
	for _, scope := range scopes {
		for _, driver := range scope.Drivers {
			for _, row := range batch.rows[scope.Week][driver] {
				if row.Source == "escrow" {
					capacities = append(capacities, capacity{scope.Week, driver, row.ExpenseID, row.Available})
				}
			}
		}
	}
	body, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	escrow, err := json.Marshal(capacities)
	if err != nil {
		return err
	}
	eligibility := strings.ReplaceAll(payDriverEligibilitySQL, "$1::date", "d.payroll_week")
	eligibility = strings.ReplaceAll(eligibility, "WHERE x.driver_id=d.id AND ((x.available", "WHERE x.driver_id=d.id AND x.week=d.payroll_week AND (x.available")
	rows, err := tx.Query(ctx, `WITH scopes AS MATERIALIZED (SELECT * FROM jsonb_to_recordset($1::jsonb) s(week date,drivers uuid[])),
 pay_drivers AS MATERIALIZED (SELECT d.*,s.week payroll_week FROM scopes s CROSS JOIN LATERAL unnest(s.drivers) wanted(id) JOIN drivers d ON d.id=wanted.id),
 pay_escrow_capacity AS MATERIALIZED (SELECT week,driver driver_id,id,available FROM jsonb_to_recordset($2::jsonb) c(week date,driver uuid,id uuid,available numeric))`+eligibility+`
 SELECT d.payroll_week::text,d.id::text,d.pay_enabled,d.pay_release,d.pay_scale,d.pay_reimburse,d.pay_charge,d.pay_expense,d.pay_escrow,
 coalesce(w.notes,''),coalesce(w.comments,'{}'::jsonb),coalesce(w.adjustments,'[]'::jsonb),coalesce(w.version,0),
 w.fuel_override::text,w.toll_override::text,coalesce(w.layover_excluded_dates,'{}'::text[])
 FROM eligible_pay_drivers d LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=d.payroll_week`, body, escrow)
	if err != nil {
		return err
	}
	defer rows.Close()
	values := map[string]map[string]payrollDriverEligibility{}
	for rows.Next() {
		var week, id string
		var v payrollDriverEligibility
		var comments, adjustments []byte
		if err = rows.Scan(&week, &id, &v.enabled, &v.release, &v.scale, &v.reimburse, &v.charge, &v.expense, &v.escrow, &v.edits.Notes, &comments, &adjustments, &v.edits.Version, &v.edits.FuelOverride, &v.edits.TollOverride, &v.edits.LayoverExcludedDates); err != nil {
			return err
		}
		if err = json.Unmarshal(comments, &v.edits.Comments); err == nil {
			err = json.Unmarshal(adjustments, &v.edits.Adjustments)
		}
		if err != nil {
			return err
		}
		v.edits.DriverID, v.edits.WeekStart = id, week
		if values[week] == nil {
			values[week] = map[string]payrollDriverEligibility{}
		}
		values[week][id] = v
	}
	if err = rows.Err(); err != nil {
		return err
	}
	publication.driverEligibility = values
	return nil
}
