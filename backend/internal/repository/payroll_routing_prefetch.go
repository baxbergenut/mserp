package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// These are complete, source-only weekly facts. Fetch the exact revision cohort
// once for a cross-period publication; each consumer still decodes its own copy.
// Missing rows remain ordinary misses and use the independent source query.
func prefetchPayrollRoutingFacts(ctx context.Context, tx pgx.Tx, weeks []time.Time) error {
	s, _ := ctx.Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	if s == nil || !payrollNormalizedInputs(ctx) || len(weeks) < 2 {
		return nil
	}
	type cohort struct {
		Kind        string `json:"kind"`
		Fingerprint string `json:"fingerprint"`
	}
	cohorts := []cohort{}
	for _, kind := range []string{"driver_days", "investor_days", "vacations", "stopped_charges", "load_trucks", "truck_costs", "native_costs"} {
		fingerprint, err := payrollRoutingFingerprint(s, kind)
		if err != nil {
			return err
		}
		cohorts = append(cohorts, cohort{kind, fingerprint})
	}
	body, err := json.Marshal(cohorts)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT f.kind,f.week_start::text,f.input_fingerprint,f.payload
 FROM jsonb_to_recordset($1::jsonb) requested(kind text,fingerprint text)
 JOIN payroll_routing_facts f ON f.kind=requested.kind AND f.input_fingerprint=requested.fingerprint
 WHERE f.week_start=ANY($2::date[]) AND f.entity_id='' AND f.calculator_version=$3`, body, weeks, payrollCalculatorVersion)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, week, fingerprint string
		var payload []byte
		if err = rows.Scan(&kind, &week, &fingerprint, &payload); err != nil {
			return err
		}
		s.values[kind+":"+week+":"+fingerprint] = payload
	}
	return rows.Err()
}
