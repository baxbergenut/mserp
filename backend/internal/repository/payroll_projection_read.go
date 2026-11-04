package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Shadow diagnostics deliberately do not replace the existing HTTP contract.
// Consumers must check State; the last complete values are not necessarily current.
type PreparedPayrollPage struct {
	Report     DriverPayWeek                     `json:"report"`
	State      string                            `json:"state"`
	Generation string                            `json:"generation,omitempty"`
	PreparedAt *time.Time                        `json:"preparedAt,omitempty"`
	Calculator string                            `json:"calculatorVersion,omitempty"`
	Statements map[string]PreparedPayrollVersion `json:"statements"`
	Failure    string                            `json:"failure,omitempty"`
}

type PreparedPayrollVersion struct {
	Revision string `json:"revision"`
	State    string `json:"state"`
}

type PayrollPreparation struct {
	State      string     `json:"state"`
	Generation string     `json:"generation,omitempty"`
	PreparedAt *time.Time `json:"preparedAt,omitempty"`
	Calculator string     `json:"calculatorVersion,omitempty"`
	Failure    string     `json:"failure,omitempty"`
}

var ErrPayrollVersionExpired = errors.New("this statement version is no longer available; refresh the statement")
var ErrPayrollPreparing = errors.New("payroll is preparing; retry shortly")

// The adapter preserves the public report shape and keeps the week's input
// revision distinct from each immutable statement version.
func (r *DriverPayRepository) PreparedPayroll(ctx context.Context, kind string, week time.Time, query PayPageQuery, aware bool) (DriverPayWeek, error) {
	page, err := r.ReadPreparedPayrollPage(ctx, kind, week, query)
	if err != nil {
		return page.Report, err
	}
	if page.State != "ready" {
		if err = r.RequestPayrollPreparation(ctx, week, false); err != nil {
			return page.Report, err
		}
		if !aware {
			return DriverPayWeek{}, ErrPayrollPreparing
		}
	}
	if query.Interactive && query.StatementRevision == "" {
		if err = r.prioritizePayrollWeek(ctx, week); err != nil {
			return page.Report, err
		}
	}
	page.Report.Preparation = &PayrollPreparation{page.State, page.Generation, page.PreparedAt, page.Calculator, page.Failure}
	for i := range page.Report.Drivers {
		d := &page.Report.Drivers[i]
		d.StatementRevision = page.Statements[d.ID].Revision
		d.PreparationState = page.Statements[d.ID].State
		d.Edits.ExpectedStatementRevision = d.StatementRevision
	}
	return page.Report, nil
}

// ReadPreparedPayrollPage executes NO calculator, even after process restart.
// Summary pagination and expanded detail share one repeatable-read snapshot.
// Current pointers and headers are read in the same snapshot. A pinned detail
// must still match current membership; superseded/removed revisions expire.
func (r *DriverPayRepository) ReadPreparedPayrollPage(ctx context.Context, kind string, week time.Time, query PayPageQuery) (PreparedPayrollPage, error) {
	result := PreparedPayrollPage{State: "preparing", Report: DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}, Statements: map[string]PreparedPayrollVersion{}}
	if kind != "driver" && kind != "investor" {
		return result, chargeInvalid("Invalid payroll kind")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var header []byte
	var snapshot, calendar, current, financialSnapshot string
	var missing []string
	var schema int
	var prepared *time.Time
	if query.StatementRevision != "" && query.ID == "" {
		return result, chargeInvalid("A statement identity is required with its version")
	}
	excluded := append(append([]string{}, payrollUnrelatedTables...), "payroll_cache_revisions", "schema_migrations", "auth_sessions", "user_login_activity")
	err = tx.QueryRow(ctx, `WITH fence AS (`+strings.ReplaceAll(payrollProjectionFenceSQL, "$1", "$5")+`), chosen AS (
 SELECT g.* FROM payroll_projection_generations g
 WHERE g.week_start=$1 AND g.id=CASE WHEN $3='' THEN (SELECT generation_id FROM payroll_projection_weeks WHERE week_start=$1)
 ELSE coalesce((SELECT w.generation_id FROM payroll_projection_weeks w JOIN payroll_statement_state s ON s.week_start=w.week_start
 WHERE s.version_id=nullif($3,'')::uuid AND s.kind=$2 AND s.statement_id=nullif($4,'')::uuid AND s.week_start=$1),
 (SELECT c.generation_id FROM payroll_statement_current c JOIN payroll_statement_state s ON s.version_id=c.id
 WHERE c.id=nullif($3,'')::uuid AND c.kind=$2 AND c.statement_id=nullif($4,'')::uuid AND c.week_start=$1)) END
 ) SELECT f.missing,f.snapshot,coalesce(payroll_week_revision($1,$2,g.id),''),coalesce(g.input_snapshot,''),coalesce(g.calendar_week::text,''),coalesce(g.calculator_version,''),coalesce(g.schema_version,0),g.prepared_at,
 CASE WHEN $2='driver' THEN g.driver_header ELSE g.investor_header END,coalesce(j.last_error,''),payroll_financial_snapshot(f.snapshot)
 FROM fence f LEFT JOIN chosen g ON true LEFT JOIN payroll_projection_jobs j ON j.week_start=$1`, week, kind, query.StatementRevision, query.ID, excluded).
		Scan(&missing, &current, &result.Generation, &snapshot, &calendar, &result.Calculator, &schema, &prepared, &header, &result.Failure, &financialSnapshot)
	if err != nil {
		return result, err
	}
	if result.Generation == "" {
		if query.StatementRevision != "" {
			return result, ErrPayrollVersionExpired
		}
		if result.Failure != "" || len(missing) > 0 {
			result.State = "failed"
			if len(missing) > 0 {
				result.Failure = "Source capture coverage is incomplete"
			}
		}
		return result, tx.Commit(ctx)
	}
	if err != nil {
		return result, err
	}
	result.PreparedAt = prepared
	if err = json.Unmarshal(header, &result.Report); err != nil {
		return result, err
	}
	result.State = "ready"
	if snapshot != current || calendar != ChargeCurrentWeek() || result.Calculator != payrollCalculatorVersion || schema != payrollProjectionSchemaVersion {
		result.State = "updating"
	}
	if len(missing) > 0 {
		result.Failure = "Source capture coverage is incomplete"
	}
	if result.Failure != "" {
		result.State = "failed"
	}
	if result.State == "ready" && query.StatementRevision == "" {
		// The header certifies source membership; independent financial commands
		// need not rewrite it. Keep the legacy input token tied to this snapshot.
		result.Report.Revision = payrollInputRevision(kind, week.Format(time.DateOnly), calendar, financialSnapshot)
	}
	// A stale weekly header does not make every statement stale. Prove the
	// complete intervening source impact once, then compare each statement's
	// exact dependency component without invoking the calculator.
	compatible := len(missing) == 0 && calendar == ChargeCurrentWeek() && result.Calculator == payrollCalculatorVersion && schema == payrollProjectionSchemaVersion
	var changedRoots []string
	var completeImpact bool
	if compatible && snapshot != current {
		changedRoots, completeImpact, err = payrollChangedSourceRoots(ctx, tx, snapshot, current)
		if err != nil {
			return result, err
		}
	}
	// Dense components share thousands of dependency revisions. Check each shared
	// map once in this read snapshot, then check every statement's local overrides.
	// Re-expanding that map for every summary made polling monopolize the database.
	rows, err := tx.Query(ctx, `WITH versions AS MATERIALIZED (
 SELECT v.id,v.summary,v.statement_id,v.resource_dependencies FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id
 WHERE $3='' AND s.week_start=$1 AND s.kind=$2
 UNION ALL SELECT v.id,v.summary,v.statement_id,v.resource_dependencies FROM payroll_statement_current v JOIN payroll_statement_state s ON s.version_id=v.id
 WHERE v.id=nullif($3,'')::uuid AND v.week_start=$1 AND v.kind=$2
 ), shared AS MATERIALIZED (
 SELECT d.fingerprint,(d.dependencies<>'{}' OR
 (SELECT protocol_mode='global' FROM payroll_projection_control WHERE singleton)) AS valid,
 d.dependencies ?| $5::text[] AS source_changed,
 ARRAY(SELECT e.key FROM jsonb_each_text(d.dependencies) e(key,value)
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=e.key
 WHERE coalesce(r.revision,0)<>e.value::bigint) AS stale_keys
 FROM payroll_resource_dependency_sets d JOIN (
 SELECT DISTINCT resource_dependencies->>'$shared' AS fingerprint FROM versions
 WHERE resource_dependencies ? '$shared') refs USING(fingerprint)
 ) SELECT v.id::text,v.summary,CASE WHEN v.resource_dependencies ? '$shared' THEN
 shared.fingerprint IS NOT NULL AND (shared.valid OR v.resource_dependencies-'$shared'<>'{}') AND NOT EXISTS(
 SELECT 1 FROM unnest(shared.stale_keys) key WHERE NOT v.resource_dependencies ? key) AND NOT EXISTS(
 SELECT 1 FROM jsonb_each_text(v.resource_dependencies-'$shared') d(key,value)
 LEFT JOIN payroll_resource_revisions r ON r.resource_key=d.key WHERE coalesce(r.revision,0)<>d.value::bigint)
 ELSE payroll_resources_current(v.resource_dependencies) END,
 $4 AND (SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton)
 AND v.resource_dependencies<>'{}'::jsonb
 AND NOT (v.resource_dependencies ?| $5::text[]) AND NOT coalesce(shared.source_changed,false)
 FROM versions v LEFT JOIN shared ON shared.fingerprint=v.resource_dependencies->>'$shared'
 ORDER BY v.summary->>'fullName',v.statement_id`, week, kind, query.StatementRevision, completeImpact, changedRoots)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var version string
		var body []byte
		var currentResources, unrelatedSources bool
		if err = rows.Scan(&version, &body, &currentResources, &unrelatedSources); err != nil {
			rows.Close()
			return result, err
		}
		if !currentResources && result.State == "ready" {
			result.State = "updating"
		}
		var d DriverPayDriver
		if err = json.Unmarshal(body, &d); err != nil {
			rows.Close()
			return result, err
		}
		result.Report.Drivers = append(result.Report.Drivers, d)
		state := result.State
		if compatible && currentResources && (snapshot == current || unrelatedSources) {
			state = "ready"
		}
		result.Statements[d.ID] = PreparedPayrollVersion{Revision: version, State: state}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	pageQuery := query
	pageQuery.Summary, pageQuery.DetailIDs = true, nil
	result.Report = PaginatePay(result.Report, pageQuery)
	details := []string{}
	visible := map[string]PreparedPayrollVersion{}
	for _, d := range result.Report.Drivers {
		visible[d.ID] = result.Statements[d.ID]
		if !query.Summary || query.DetailIDs[d.ID] {
			details = append(details, result.Statements[d.ID].Revision)
		}
	}
	result.Statements = visible
	if len(details) != 0 {
		rows, err = tx.Query(ctx, `SELECT detail FROM payroll_statement_versions WHERE id=ANY($1::uuid[])`, details)
		if err != nil {
			return result, err
		}
		byID := map[string]DriverPayDriver{}
		for rows.Next() {
			var body []byte
			if err = rows.Scan(&body); err != nil {
				rows.Close()
				return result, err
			}
			var d DriverPayDriver
			if err = json.Unmarshal(body, &d); err != nil {
				rows.Close()
				return result, err
			}
			byID[d.ID] = d
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return result, err
		}
		for i, d := range result.Report.Drivers {
			if detail, ok := byID[d.ID]; ok {
				result.Report.Drivers[i] = detail
			}
		}
	}
	if query.ID != "" && len(result.Report.Drivers) == 1 && result.Statements[query.ID].State == "ready" {
		result.State = "ready"
	}
	if err = decorateFrozenSourceComparisons(ctx, tx, &result.Report); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
