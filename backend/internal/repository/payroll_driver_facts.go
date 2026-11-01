package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reuse source loads, tariffs, dated employment/dispatch and native cost totals.
// Current eligibility and all saved financial fields come from canonical rows in
// this transaction. Frozen reports remain a later overlay, including on reopen.
func readDriverPayLiveBaseWeek(ctx context.Context, tx pgx.Tx, week time.Time, driverID string) (DriverPayWeek, error) {
	facts, _ := ctx.Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	if facts == nil || !payrollNormalizedInputs(ctx) || payrollSummaryOnly(ctx) || payrollCostsOnly(ctx) || payrollCostDetails(ctx) || driverID != "" {
		return readDriverPayBaseWeek(ctx, tx, week, driverID, false)
	}
	result, err := readPreparedDriverFact(ctx, tx, week, facts, "driver_base", func() (DriverPayWeek, error) {
		return readDriverPayBaseWeek(ctx, tx, week, "", true)
	})
	if err != nil {
		return result, err
	}
	current, err := readPayrollDriverEligibility(ctx, tx, week, driverID, result.WeekStart)
	if err != nil {
		return result, err
	}
	kept := []DriverPayDriver{}
	for _, d := range result.Drivers {
		v, selected := current[d.ID]
		if !selected {
			continue
		}
		hasLoads, saved := len(d.Loads) > 0, v.edits.Version > 0
		first := v.enabled || hasLoads || saved || v.release || v.scale || v.reimburse
		fuel, _ := chargeCents(d.FuelTotal)
		toll, _ := chargeCents(d.TollTotal)
		second := v.reimburse || v.scale || v.release || hasLoads || saved || v.charge || v.expense || v.escrow ||
			(d.IsOwnerOperator && d.PayType == "gross_percentage" && (fuel != 0 || toll != 0))
		if first && second {
			d.Edits = v.edits
			kept = append(kept, d)
		}
	}
	result.Drivers = kept
	return result, nil
}

// Store source cards separately. A one-driver command reads only its complete
// dependency group, rather than decoding a full fleet's source loads. The empty
// entity is a completeness marker committed with every card, including an empty
// fleet; absence is a cache miss, never evidence of ineligibility.
func readPreparedDriverFact(ctx context.Context, tx pgx.Tx, week time.Time, facts *payrollRoutingFactsState, kind string, compute func() (DriverPayWeek, error)) (DriverPayWeek, error) {
	result := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	fingerprint, err := payrollRoutingFingerprint(facts, kind)
	if err != nil {
		return result, err
	}
	var complete bool
	var body []byte
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_routing_facts WHERE kind=$5 AND week_start=$1 AND calculator_version=$2 AND input_fingerprint=$3 AND entity_id=''),
 coalesce((SELECT jsonb_agg(payload ORDER BY payload->>'fullName',entity_id) FROM payroll_routing_facts
 WHERE kind=$5 AND week_start=$1 AND calculator_version=$2 AND input_fingerprint=$3
 AND entity_id<>'' AND ($4::text[] IS NULL OR entity_id=ANY($4::text[]))),'[]'::jsonb)`, week, payrollCalculatorVersion, fingerprint, payrollDriverIDs(ctx), kind).Scan(&complete, &body)
	if err != nil {
		return result, err
	}
	if complete {
		err = json.Unmarshal(body, &result.Drivers)
		return result, err
	}
	result, err = compute()
	if err != nil || payrollDriverIDs(ctx) != nil || payrollTruckIDs(ctx) != nil {
		return result, err
	}
	body, err = json.Marshal(result.Drivers)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO payroll_routing_facts(kind,week_start,calculator_version,input_fingerprint,entity_id,payload)
 SELECT $5,$1::date,$2,$3,card->>'id',card FROM jsonb_array_elements($4::jsonb) card
 UNION ALL SELECT $5,$1::date,$2,$3,'','{}'::jsonb ON CONFLICT DO NOTHING`, week, payrollCalculatorVersion, fingerprint, body, kind)
	return result, err
}
