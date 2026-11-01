package repository

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Freeze-only commands already produced the canonical post-write cards. The
// captured-event proof excludes allocation/confirmation changes and legacy cost
// bases. In resource mode no shared header needs rebasing. Preserve original
// cost evidence and unrelated versions, and publish the frozen cards in a batch.
func publishPayrollFrozen(ctx context.Context, tx pgx.Tx, c *payrollCommand, impact *payrollPublicationImpact, kind string, report DriverPayWeek, selected map[string]bool) ([]PayrollStatementIdentity, bool, error) {
	if !payrollConcurrent(tx) || c == nil || c.expected == "" || impact == nil || impact.finalizedKind != kind ||
		len(impact.weeks) != 1 || !impact.weeks[report.WeekStart] || len(report.Drivers) == 0 || len(report.Drivers) != len(selected) {
		return nil, false, nil
	}
	type frozen struct {
		ID                string          `json:"id"`
		Detail            DriverPayDriver `json:"detail"`
		Summary           DriverPayDriver `json:"summary"`
		Fee               int64           `json:"fee"`
		Payable           int64           `json:"payable"`
		EditVersion       int             `json:"edit_version"`
		SettlementVersion int             `json:"settlement_version"`
	}
	rows := make([]frozen, 0, len(report.Drivers))
	identities := make([]PayrollStatementIdentity, 0, len(report.Drivers))
	for _, d := range report.Drivers {
		if !selected[d.ID] || d.Settlement == nil || !d.Settlement.Finalized {
			return nil, false, nil
		}
		summary := summarizePayStatement(d)
		fee, err := chargeCents(summary.Summary.Fee)
		if err != nil {
			return nil, false, err
		}
		payable, err := chargeCents(summary.Summary.Payable)
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, frozen{d.ID, d, summary, fee, payable, d.Edits.Version, d.Settlement.Version})
		identities = append(identities, PayrollStatementIdentity{kind, d.ID, report.WeekStart})
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return nil, false, err
	}
	var published int
	err = tx.QueryRow(ctx, `WITH generation AS (
 INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT g.week_start,g.requested_generation,g.input_snapshot,g.calendar_week,g.calculator_version,g.schema_version,g.dependencies,g.driver_header,g.investor_header
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id
 WHERE w.week_start=$1::date AND g.input_snapshot=$2 AND g.calendar_week=$3::date AND g.calculator_version=$4 AND g.schema_version=$5
 RETURNING id
 ), versions AS (
 INSERT INTO payroll_statement_versions(generation_id,kind,statement_id,week_start,detail,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,cost_evidence,resource_dependencies)
 SELECT g.id,$6,s.statement_id,s.week_start,r.detail,r.summary,r.fee,r.payable,true,r.edit_version,r.settlement_version,v.cost_evidence,
 payroll_resource_snapshot(ARRAY(SELECT jsonb_object_keys(v.resource_dependencies)))
 FROM jsonb_to_recordset($7::jsonb) r(id uuid,detail jsonb,summary jsonb,fee bigint,payable bigint,edit_version int,settlement_version int)
 JOIN payroll_statement_state s ON s.kind=$6 AND s.statement_id=r.id AND s.week_start=$1::date
 JOIN payroll_statement_versions v ON v.id=s.version_id CROSS JOIN generation g
 WHERE v.fee_cents=r.fee AND v.payable_cents=r.payable
 AND v.detail->'fuelTotal' IS NOT DISTINCT FROM r.detail->'fuelTotal'
 AND v.detail->'tollTotal' IS NOT DISTINCT FROM r.detail->'tollTotal'
 AND v.detail#>'{edits,costs}' IS NOT DISTINCT FROM r.detail#>'{edits,costs}'
 RETURNING id,statement_id
 ), pointers AS (
 UPDATE payroll_statement_state s SET version_id=v.id FROM versions v
 WHERE s.kind=$6 AND s.week_start=$1::date AND s.statement_id=v.statement_id RETURNING s.version_id
 ) SELECT count(*) FROM pointers`, report.WeekStart, c.beforeSnapshot, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion, kind, body).Scan(&published)
	if err != nil {
		return nil, false, err
	}
	if published != len(rows) {
		return nil, false, ErrDriverPayConflict
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_changes SET dispatched_at=clock_timestamp() WHERE source_xid=pg_current_xact_id() AND dispatched_at IS NULL`)
	sort.Slice(identities, func(i, j int) bool { return identities[i].ID < identities[j].ID })
	return identities, true, err
}
