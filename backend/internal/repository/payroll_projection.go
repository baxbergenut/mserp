package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mserp/internal/db"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Bump whenever calculation/display semantics or the persisted shape changes.
// These versions are independent of both canonical edit versions and job leases.
const payrollCalculatorVersion = "payroll-198-v2"
const payrollProjectionSchemaVersion = 12

var ErrPayrollProjectionCoverage = errors.New("payroll projection source capture is incomplete")
var ErrPayrollProjectionSuperseded = errors.New("payroll projection inputs or lease changed")
var errPayrollProjectionNeedsFullClaims = fmt.Errorf("%w: queued resource claims are incomplete", ErrPayrollProjectionSuperseded)

type payrollProjectionJob struct {
	Week       time.Time
	Generation int64
	Token      string
	Roots      []string
}

// Coverage checks event semantics as well as names. Merely registering a table,
// or retaining a disabled trigger, is not proof of transactional invalidation.
func payrollProjectionCoverage(ctx context.Context, tx pgx.Tx) error {
	_, err := payrollProjectionFence(ctx, tx)
	return err
}
func payrollProjectionFence(ctx context.Context, tx pgx.Tx) (string, error) {
	if resource, ok := tx.(*payrollResourceTransaction); ok && resource.sourceFence != nil {
		return *resource.sourceFence, nil
	}
	var missing []string
	var snapshot string
	excluded := append(append([]string{}, payrollUnrelatedTables...), "payroll_cache_revisions", "schema_migrations", "auth_sessions", "user_login_activity")
	err := tx.QueryRow(ctx, payrollProjectionFenceSQL, excluded).Scan(&missing, &snapshot)
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("%w: %s", ErrPayrollProjectionCoverage, strings.Join(missing, ", "))
	}
	if resource, ok := tx.(*payrollResourceTransaction); ok {
		// Only this transaction type holds the shared source gate and exact
		// resource claims. Coverage above verifies that its writes cannot change
		// source counters. Schema/protocol changes require quiesced writers.
		resource.sourceFence = &snapshot
	}
	return snapshot, nil
}

// Catalog coverage and committed source revisions are one snapshot read. This
// also supplies page freshness without separate existence/registry queries.
const payrollProjectionFenceSQL = `WITH tables AS MATERIALIZED (
 SELECT oid,relname AS tablename,relnamespace AS namespace FROM pg_class
 WHERE relnamespace=current_schema()::regnamespace AND relkind IN ('r','p')
 ), missing AS (
 SELECT t.tablename
 FROM tables t LEFT JOIN payroll_projection_sources s ON s.table_name=t.tablename
 WHERE (s.table_name IS NULL OR
 (s.classification IN ('unrelated','derived') AND NOT s.table_name=ANY($1::text[])) OR
 (s.classification IN ('financial','display') AND (
 NOT EXISTS(SELECT 1 FROM payroll_cache_revisions r WHERE r.table_name=t.tablename) OR EXISTS(
 SELECT 1 FROM (VALUES(4,'insert'),(8,'delete'),(16,'update'),(32,'truncate')) required(op,name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE g.tgrelid=t.oid
 AND g.tgname='payroll_projection_'||required.name AND p.proname='capture_payroll_projection_change'
 AND p.pronamespace=t.namespace AND g.tgenabled IN ('O','A') AND g.tgtype=required.op
 AND (required.op NOT IN (4,16) OR g.tgnewtable='changed_rows')
 AND (required.op NOT IN (8,16) OR g.tgoldtable='previous_rows'))
 OR NOT EXISTS(SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE g.tgrelid=t.oid
 AND (p.proname IN ('invalidate_payroll_cache','invalidate_payroll_load_cache') OR
 (t.tablename='fleet_status_history' AND p.proname='invalidate_payroll_status_history_cache'
 AND p.pronamespace=t.namespace AND (required.op NOT IN (8,16) OR g.tgoldtable='previous_rows')
 AND (required.op NOT IN (4,16) OR g.tgnewtable='changed_rows')) OR
 (t.tablename IN ('expenses','app_users','tolls','weighmytruck_transactions') AND required.op=16
 AND p.proname=CASE t.tablename WHEN 'expenses' THEN 'invalidate_payroll_expense_cache' WHEN 'app_users' THEN 'invalidate_payroll_user_cache' ELSE 'invalidate_payroll_import_cache' END
 AND p.pronamespace=t.namespace AND g.tgoldtable='previous_rows' AND g.tgnewtable='changed_rows'))
 AND g.tgenabled IN ('O','A') AND g.tgtype=required.op)))))
 UNION SELECT 'protocol guards: '||t.tablename FROM tables t JOIN payroll_projection_sources s ON s.table_name=t.tablename
 WHERE s.classification IN ('financial','display') AND (SELECT protocol_mode<>'global' FROM payroll_projection_control WHERE singleton)
 AND (NOT EXISTS(SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE g.tgrelid=t.oid AND g.tgname='payroll_protocol_statement' AND g.tgtype=62 AND g.tgenabled IN ('O','A')
 AND p.proname='guard_payroll_protocol_statement' AND p.pronamespace=t.namespace)
 OR (payroll_resource_table(t.tablename) AND NOT EXISTS(SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE g.tgrelid=t.oid AND g.tgname='payroll_protocol_row' AND g.tgtype=31 AND g.tgenabled IN ('O','A')
 AND p.proname='guard_payroll_protocol_row' AND p.pronamespace=t.namespace)))
 UNION SELECT 'operational board history guard' WHERE NOT EXISTS(
 SELECT 1 FROM tables t JOIN pg_trigger g ON g.tgrelid=t.oid JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE t.tablename='fleet_status_history' AND g.tgname='board_operational_history' AND g.tgtype=31
 AND g.tgenabled IN ('O','A') AND p.proname='guard_board_operational_history' AND p.pronamespace=t.namespace)
 UNION SELECT 'account metadata guard' WHERE NOT EXISTS(
 SELECT 1 FROM tables t JOIN pg_trigger g ON g.tgrelid=t.oid JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE t.tablename='app_users' AND g.tgname='zz_payroll_user_metadata' AND g.tgtype=31
 AND g.tgenabled IN ('O','A') AND p.proname='guard_payroll_user_metadata' AND p.pronamespace=t.namespace)
 UNION SELECT 'balance bucket maintenance' WHERE EXISTS(SELECT 1 FROM (VALUES('driver_pay_cost_collections'),('pay_source_collections')) t(name)
 CROSS JOIN (VALUES('payroll_balance_rows',29),('payroll_balance_truncate',32)) required(name,events)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid=g.tgfoid
 WHERE g.tgrelid=(quote_ident(current_schema())||'.'||quote_ident(t.name))::regclass
 AND g.tgname=required.name AND g.tgtype=required.events AND g.tgenabled IN ('O','A')
 AND p.proname='maintain_payroll_balance_months' AND p.pronamespace=current_schema()::regnamespace))
 ) SELECT ARRAY(SELECT tablename FROM missing ORDER BY tablename) missing,
 coalesce((SELECT string_agg(table_name||':'||revision::text,',' ORDER BY table_name) FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[])),'') snapshot`

// Enable only capture/shadow preparation. This performs no canonical financial
// writes and does not switch any production read, command, or lock protocol.
func (r *DriverPayRepository) EnablePayrollShadow(ctx context.Context) error {
	if err := r.loadPayrollFirstWeek(ctx); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = payrollProjectionCoverage(ctx, tx); err != nil {
		return err
	}
	changed, err := tx.Exec(ctx, `UPDATE payroll_projection_control SET capture_enabled=true WHERE singleton AND NOT capture_enabled`)
	if err != nil {
		return err
	}
	if changed.RowsAffected() != 0 {
		// Fence transactions whose repeatable snapshot predates capture activation.
		// Such a writer cannot silently commit against the old disabled control:
		// updating its changed revision row will require a new transaction.
		_, err = tx.Exec(ctx, `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE NOT table_name=ANY($1::text[])`, payrollUnrelatedTables)
		if err != nil {
			return err
		}
	}
	// A previous process may have run with capture disabled. Reconcile every
	// retained projection on startup; an empty outbox cannot prove it current.
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,impact_roots=NULL,
 requested_at=CASE WHEN requested_generation=completed_generation THEN clock_timestamp() ELSE requested_at END,
 available_at=clock_timestamp()`)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Enqueue a week only when missing or explicitly retried. Ordinary readers must
// not increment generations on every poll and starve the running calculation.
func (r *DriverPayRepository) RequestPayrollPreparation(ctx context.Context, week time.Time, retry bool) error {
	if _, err := chargeWeek(week.Format(time.DateOnly)); err != nil {
		return err
	}
	if week.Format(time.DateOnly) > PayrollLatestWeek() {
		return chargeInvalid("Payroll weeks are available after the week ends")
	}
	if week.Format(time.DateOnly) < r.FirstPayrollWeek() {
		return chargeInvalid("This payroll week has been retired")
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO payroll_projection_jobs(week_start,priority) VALUES($1,100)
 ON CONFLICT(week_start) DO UPDATE SET priority=greatest(payroll_projection_jobs.priority,100),
 impact_roots=CASE WHEN $2 THEN NULL ELSE payroll_projection_jobs.impact_roots END,
 requested_generation=payroll_projection_jobs.requested_generation+CASE WHEN $2 THEN 1 ELSE 0 END,
 last_error=CASE WHEN $2 THEN '' ELSE payroll_projection_jobs.last_error END,
 available_at=CASE WHEN $2 THEN clock_timestamp() ELSE payroll_projection_jobs.available_at END,
 requested_at=CASE WHEN $2 AND payroll_projection_jobs.requested_generation=payroll_projection_jobs.completed_generation
 THEN clock_timestamp() ELSE payroll_projection_jobs.requested_at END
 WHERE $2`, week, retry)
	return err
}

// Keep visible weeks ahead of routine current-week/history work when a source
// commit arrives. The lease expires after navigation; polling renews it at most
// once per five seconds, and never rewrites a job while a worker holds its lease.
// This hint never requests another generation or changes
// financial data, and the read's freshness result still comes from its snapshot.
func (r *DriverPayRepository) prioritizePayrollWeek(ctx context.Context, week time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE payroll_projection_jobs SET interactive_until=clock_timestamp()+interval '10 seconds'
 WHERE week_start=$1 AND (lease_until IS NULL OR lease_until<clock_timestamp())
 AND (interactive_until IS NULL OR interactive_until<clock_timestamp()+interval '5 seconds')`, week)
	return err
}

// Dispatch exact committed rows, never an ID high-water mark: a transaction that
// allocated a smaller sequence value can still commit after this batch.
func (r *DriverPayRepository) DispatchPayrollChanges(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM payroll_projection_changes WHERE dispatched_at IS NULL ORDER BY id LIMIT 256 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(ids) == 0 {
		return 0, err
	}
	impact, err := payrollCapturedImpact(ctx, tx, nil, "", "", ids)
	if err != nil {
		return 0, err
	}
	var roots []string
	var weeks []string
	if impact != nil {
		// Empty means a proven unaffected period below. An affected change
		// without resolved principals must retain the full-calculation fallback.
		if len(impact.roots) > 0 {
			roots = impact.roots
		}
		if impact.weeks != nil {
			weeks = []string{}
			for week := range impact.weeks {
				weeks = append(weeks, week)
			}
		}
	}
	// Coalesce old and new principals across every pending batch. An unknown
	// batch or an initial/full rebuild remains full until that job completes.
	_, err = tx.Exec(ctx, `WITH impact AS (
 SELECT week_start,CASE WHEN $3::text[] IS NOT NULL AND NOT week_start::text=ANY($3) THEN ARRAY[]::text[] ELSE $2::text[] END roots
 FROM payroll_projection_jobs WHERE week_start<=$1::date
 ) UPDATE payroll_projection_jobs j SET requested_generation=requested_generation+1,
 impact_roots=CASE WHEN i.roots IS NULL THEN NULL WHEN requested_generation=completed_generation THEN i.roots
 WHEN impact_roots IS NULL THEN NULL ELSE ARRAY(SELECT DISTINCT root FROM unnest(impact_roots||i.roots) root ORDER BY root) END,
 requested_at=CASE WHEN requested_generation=completed_generation THEN clock_timestamp() ELSE requested_at END,
 available_at=clock_timestamp(),last_error='',
 priority=greatest(priority,CASE WHEN j.week_start=$1::date THEN 90
 WHEN j.week_start BETWEEN $1::date-56 AND $1::date THEN 80-abs((j.week_start-$1::date)/7) ELSE 10 END)
 FROM impact i WHERE j.week_start=i.week_start`, PayrollLatestWeek(), roots, weeks)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_changes SET dispatched_at=clock_timestamp() WHERE id=ANY($1::bigint[])`, ids)
	if err != nil {
		return 0, err
	}
	return len(ids), tx.Commit(ctx)
}

func (r *DriverPayRepository) claimPayrollProjection(ctx context.Context) (payrollProjectionJob, error) {
	var job payrollProjectionJob
	err := r.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT week_start FROM payroll_projection_jobs WHERE requested_generation>completed_generation
 AND week_start<=$1::date AND week_start>=(SELECT coalesce(first_week,'-infinity'::date) FROM payroll_projection_control WHERE singleton)
 AND available_at<=clock_timestamp() AND (lease_until IS NULL OR lease_until<clock_timestamp())
 ORDER BY greatest(priority,CASE WHEN interactive_until>clock_timestamp() THEN 110 ELSE 0 END) DESC,available_at,week_start DESC LIMIT 1 FOR UPDATE SKIP LOCKED
 ) UPDATE payroll_projection_jobs j SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',attempts=attempts+1
 FROM candidate c WHERE j.week_start=c.week_start RETURNING j.week_start,j.requested_generation,j.lease_token::text,j.impact_roots`, PayrollLatestWeek()).Scan(&job.Week, &job.Generation, &job.Token, &job.Roots)
	return job, err
}

func (r *DriverPayRepository) failPayrollProjection(ctx context.Context, job payrollProjectionJob, cause error) error {
	message := "Preparation failed; see server logs"
	if errors.Is(cause, ErrPayrollProjectionCoverage) {
		message = "Source capture coverage is incomplete"
	} else if errors.Is(cause, ErrPayrollProjectionSuperseded) {
		message = ""
	}
	var pgerr *pgconn.PgError
	if errors.As(cause, &pgerr) && (pgerr.Code == "40001" || pgerr.Code == "40P01") {
		message = ""
	}
	fullClaims := errors.Is(cause, errPayrollProjectionNeedsFullClaims)
	// With event-driven pages, a real failure transition must be visible too.
	// Lease churn and superseded attempts must not generate periodic refreshes.
	_, err := r.pool.Exec(ctx, `WITH previous AS MATERIALIZED (
 SELECT last_error FROM payroll_projection_jobs WHERE week_start=$1 AND lease_token=$2::uuid
 ), failed AS (
 UPDATE payroll_projection_jobs SET lease_token=NULL,lease_until=NULL,last_error=$3,
 impact_roots=CASE WHEN $4 AND requested_generation=$5 THEN NULL ELSE impact_roots END,
 available_at=clock_timestamp()+make_interval(secs=>CASE WHEN $3='' THEN 0 ELSE least(30,power(2,least(attempts,5))::int) END)
 WHERE week_start=$1 AND lease_token=$2::uuid RETURNING week_start
 ) SELECT pg_notify('payroll_projection_visible','') FROM failed,previous WHERE previous.last_error IS DISTINCT FROM $3`, job.Week, job.Token, message, fullClaims, job.Generation)
	return err
}

func readPayrollProjectionReference(ctx context.Context, tx pgx.Tx, week time.Time, authoritative bool) ([2]DriverPayWeek, error) {
	if authoritative {
		defer db.Phase(ctx, "projection_reconciliation")()
	}
	var reports [2]DriverPayWeek
	ctx = context.WithValue(ctx, payrollAuthoritativeKey{}, authoritative)
	// Both pages use the same source/routing inputs in one uninterrupted read
	// phase. The independent authoritative reference always starts without them.
	ctx = context.WithValue(ctx, payrollReadKey{}, (*payrollReadState)(nil))
	if known := payrollRetainedPostWrite(ctx, tx, week); known != nil && known.read != nil {
		ctx = context.WithValue(ctx, payrollReadKey{}, known.read)
	}
	ctx = payrollReadContext(ctx, tx)
	if !authoritative {
		memo := payrollReadMemo(ctx, tx)
		if memo.projectionSources == nil {
			memo.projectionSources = map[string][]byte{}
		}
		if memo.projectionRoutes == nil {
			memo.projectionRoutes = map[string][]byte{}
		}
	}
	var err error
	var reused bool
	reports[0], reused, err = payrollPostWriteReport(ctx, tx, "driver", week)
	if err == nil && !reused {
		reports[0], err = readDriverPayWeek(ctx, tx, week, "")
	}
	if err != nil {
		return reports, err
	}
	reports[1], reused, err = payrollPostWriteReport(ctx, tx, "investor", week)
	if err == nil && !reused {
		reports[1], err = readInvestorPay(ctx, tx, week)
	}
	if err != nil {
		return reports, err
	}
	for i, kind := range []string{"driver", "investor"} {
		if err = decoratePayHomeWeeks(ctx, tx, kind, &reports[i]); err != nil {
			return reports, err
		}
	}
	return reports, nil
}

func samePayrollProjection(a, b [2]DriverPayWeek) bool {
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	return ex == nil && ey == nil && bytes.Equal(x, y)
}

const payrollJobSelectionSQL = `SELECT EXISTS(SELECT 1 FROM payroll_projection_weeks w
 JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1
 AND g.calendar_week=$2::date AND g.calculator_version=$3 AND g.schema_version=$4)
 AND NOT EXISTS(SELECT 1 FROM payroll_projection_changes WHERE dispatched_at IS NULL)`

// Calculation and publication use one repeatable-read snapshot. SHARE locks on
// the dependency revisions fence concurrent source commits before publication.
// PostgreSQL rejects changed rows with serialization_failure. No financial
// command is retried by this derived-work worker.
func (r *DriverPayRepository) preparePayrollProjection(ctx context.Context, job payrollProjectionJob) error {
	var resourceProtocol bool
	if err := r.pool.QueryRow(ctx, `SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton`).Scan(&resourceProtocol); err != nil {
		return err
	}
	var tx pgx.Tx
	var err error
	var closeWrite func()
	selectedResources := false
	var lockedSelection *payrollSelection
	if resourceProtocol {
		// A complete captured group only publishes its component. The independent
		// reference may read other components in the same snapshot, but it never
		// replaces their pointers. Unknown impact/backfill claims every principal.
		var roots []string
		if job.Roots != nil {
			if err = r.pool.QueryRow(ctx, payrollJobSelectionSQL, job.Week, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion).Scan(&selectedResources); err != nil {
				return err
			}
			if selectedResources {
				roots = job.Roots
			}
		}
		tx, closeWrite, err = r.beginPayrollWriteProtocol(ctx, true, "", "", roots...)
		if err == nil {
			defer closeWrite()
			lockedSelection = tx.(*payrollResourceTransaction).resources.selection
			tx = tx.(*payrollResourceTransaction).Tx
		}
	} else {
		tx, err = r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	}
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = payrollProjectionCoverage(ctx, tx); err != nil {
		return err
	}
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	if strings.HasPrefix(snapshot, "snapshot:") {
		return ErrPayrollProjectionCoverage
	}
	calendar := ChargeCurrentWeek()
	if err = preparePayrollFuelSelectionFacts(ctx, tx, snapshot); err != nil {
		return err
	}
	if err = preparePayrollLoadFacts(ctx, tx); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, payrollNormalizedFactsKey{}, true)
	readCtx := context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{tx, snapshot})
	readCtx = payrollRoutingFactsContext(readCtx, snapshot)
	rebaseOnly := false
	var impact *payrollPublicationImpact
	if job.Roots != nil {
		impact, err = payrollPendingWeekImpact(ctx, tx, job.Week, snapshot)
		if err != nil {
			return err
		}
	}
	if selectedResources && (impact == nil || !payrollSelectionCovers(lockedSelection, impact.roots)) {
		// The combined proof can be broader than the union of dispatched
		// batches. Release these locks and retry with full claims.
		return errPayrollProjectionNeedsFullClaims
	}
	if impact != nil {
		rebaseOnly = len(impact.roots) == 0 || (impact.weeks != nil && !impact.weeks[job.Week.Format(time.DateOnly)])
		if rebaseOnly {
			readCtx = context.WithValue(readCtx, payrollSelectionKey{}, &payrollSelection{drivers: []string{}, trucks: []string{}, owners: []string{}})
		} else {
			readCtx, err = selectPayrollContexts(payrollWeekContext(readCtx, job.Week.Format(time.DateOnly)), tx, "", impact.roots)
		}
		if err != nil {
			return err
		}
		selection, _ := readCtx.Value(payrollSelectionKey{}).(*payrollSelection)
		readCtx = context.WithValue(readCtx, payrollPublicationSelectionKey{}, selection)
		readCtx = context.WithValue(readCtx, payrollPublicationHeaderKey{}, !rebaseOnly)
	}
	var reports [2]DriverPayWeek
	if rebaseOnly {
		for i, kind := range []string{"driver", "investor"} {
			reports[i].WeekStart = job.Week.Format(time.DateOnly)
			if err = setPayrollRevision(readCtx, tx, kind, &reports[i]); err != nil {
				return err
			}
		}
	} else {
		reports, err = readPayrollProjectionReference(readCtx, tx, job.Week, false)
	}
	if err != nil {
		return err
	}
	selection, _ := readCtx.Value(payrollPublicationSelectionKey{}).(*payrollSelection)
	// Shadow rollout reconciles every result. After the prepared-command gate,
	// complete captured components use the verified incremental path. Backfill,
	// uncertain impact and missing coverage retain full reference reconciliation.
	reconcile := !r.preparedReads || !r.preparedCommands || selection == nil
	var authority [2]DriverPayWeek
	if reconcile {
		authorityCtx := context.WithValue(readCtx, payrollSelectionKey{}, (*payrollSelection)(nil))
		authority, err = readPayrollProjectionReference(authorityCtx, tx, job.Week, true)
		if err != nil {
			return err
		}
		if selection == nil && !samePayrollProjection(reports, authority) {
			return errors.New("shadow payroll differs from authoritative full-input calculation")
		}
	}
	// Deterministic revision-lock order. Do not memoize a post-write snapshot.
	rows, err := tx.Query(ctx, `SELECT table_name,revision FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[]) ORDER BY table_name FOR SHARE`, payrollUnrelatedTables)
	if err != nil {
		return err
	}
	dependencies := map[string]int64{}
	for rows.Next() {
		var table string
		var revision int64
		if err = rows.Scan(&table, &revision); err != nil {
			rows.Close()
			return err
		}
		dependencies[table] = revision
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if calendar != ChargeCurrentWeek() {
		return ErrPayrollProjectionSuperseded
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT coalesce(lease_token=$2::uuid AND lease_until>clock_timestamp() AND requested_generation=$3,false)
 FROM payroll_projection_jobs WHERE week_start=$1 FOR UPDATE`, job.Week, job.Token, job.Generation).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrPayrollProjectionSuperseded
	}
	if err = writePayrollProjection(readCtx, tx, job.Week, job.Generation, snapshot, calendar, dependencies, reports); err != nil {
		return err
	}
	if selection != nil && reconcile {
		published, err := readPublishedPayrollReports(ctx, tx, job.Week)
		if err != nil {
			return err
		}
		if !samePayrollProjection(published, authority) {
			return errors.New("incremental shadow payroll differs from authoritative full-input calculation")
		}
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_jobs SET completed_generation=$3,lease_token=NULL,lease_until=NULL,attempts=0,last_error='',priority=0
 WHERE week_start=$1 AND lease_token=$2::uuid`, job.Week, job.Token, job.Generation)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Reconciliation reads the actual current pointers, including retained rows
// outside an incremental publication, with the same live expense overlay as HTTP
// reads. Prepared wages, source costs and frozen statements remain authoritative.
func readPublishedPayrollReports(ctx context.Context, tx pgx.Tx, week time.Time) ([2]DriverPayWeek, error) {
	var result [2]DriverPayWeek
	var driver, investor []byte
	err := tx.QueryRow(ctx, `SELECT g.driver_header,g.investor_header FROM payroll_projection_weeks w
 JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1`, week).Scan(&driver, &investor)
	if err != nil {
		return result, err
	}
	for i, body := range [][]byte{driver, investor} {
		if err = json.Unmarshal(body, &result[i]); err != nil {
			return result, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT s.kind,v.detail FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id WHERE s.week_start=$1`, week)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var body []byte
		if err = rows.Scan(&kind, &body); err != nil {
			return result, err
		}
		var row DriverPayDriver
		if err = json.Unmarshal(body, &row); err != nil {
			return result, err
		}
		i := 0
		if kind == "investor" {
			i = 1
		}
		result[i].Drivers = append(result[i].Drivers, row)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = overlayDriverExpenseBalances(ctx, tx, &result[0]); err != nil {
		return result, err
	}
	for i, kind := range []string{"driver", "investor"} {
		if err = setPayrollRevision(ctx, tx, kind, &result[i]); err != nil {
			return result, err
		}
	}
	for k := range result {
		sort.SliceStable(result[k].Drivers, func(i, j int) bool {
			a, b := result[k].Drivers[i], result[k].Drivers[j]
			if a.FullName != b.FullName {
				return a.FullName < b.FullName
			}
			if k == 0 {
				return a.ID < b.ID
			}
			if a.InvestorID != b.InvestorID {
				return a.InvestorID < b.InvestorID
			}
			return a.TruckUnit < b.TruckUnit
		})
	}
	return result, rows.Err()
}

// writePayrollProjection publishes within the caller transaction; it never commits.
func writePayrollProjection(ctx context.Context, tx pgx.Tx, week time.Time, requestedGeneration int64, snapshot, calendar string, dependencies map[string]int64, reports [2]DriverPayWeek) error {
	ctx = context.WithValue(ctx, payrollCostTraceKey{}, &payrollCostTrace{tx: tx, weeks: map[string][2]DriverPayWeek{}})
	depJSON, err := json.Marshal(dependencies)
	if err != nil {
		return err
	}
	headers := reports
	selection, _ := ctx.Value(payrollPublicationSelectionKey{}).(*payrollSelection)
	// The period impact selection already loaded the complete relationship
	// graph. Reuse its edges, but traverse each statement's own component;
	// copying the union of command roots would overstate freshness dependencies.
	graphMemo := &payrollSelectionGraphMemo{graphs: map[string]*payrollSelectionGraph{}}
	if selection != nil && selection.graph != nil && selection.graph.tx == tx && selection.graph.week == week.Format(time.DateOnly) {
		graphMemo.graphs[selection.graph.week] = selection.graph
	}
	ctx = context.WithValue(ctx, payrollSelectionGraphKey{}, graphMemo)
	refreshHeader, _ := ctx.Value(payrollPublicationHeaderKey{}).(bool)
	if selection != nil && refreshHeader {
		// The selected calculation queries current fleet-wide setup candidates.
		// Exclude statements outside its component using their retained pointers;
		// selected statements were already excluded by the calculator itself.
		rows, err := tx.Query(ctx, `SELECT statement_id::text FROM payroll_statement_state
 WHERE week_start=$1 AND kind='investor' AND NOT statement_id=ANY($2::uuid[])`, week, selection.trucks)
		if err != nil {
			return err
		}
		retained := map[string]bool{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			retained[id] = true
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		setup := []InvestorTruckSetup{}
		for _, row := range headers[1].SetupRequired {
			if !retained[row.TruckID] {
				setup = append(setup, row)
			}
		}
		headers[1].SetupRequired = setup
	} else if selection != nil {
		var driver, investor []byte
		if err = tx.QueryRow(ctx, `SELECT g.driver_header,g.investor_header FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1`, week).Scan(&driver, &investor); err != nil {
			return err
		}
		if err = json.Unmarshal(driver, &headers[0]); err != nil {
			return err
		}
		if err = json.Unmarshal(investor, &headers[1]); err != nil {
			return err
		}
		// This path is restricted to ledger writes; fleet-wide setup and notices
		// cannot change. Row totals are always derived from published summaries.
		for i := range headers {
			headers[i].Revision = reports[i].Revision
		}
	}
	for i := range headers {
		headers[i].Drivers = []DriverPayDriver{}
	}
	driverHeader, _ := json.Marshal(headers[0])
	investorHeader, _ := json.Marshal(headers[1])
	var generation string
	var resourceProtocol bool
	err = tx.QueryRow(ctx, `INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,
 calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 VALUES($1,$2,$3,$4::date,$5,$6,$7,$8,$9) RETURNING id::text,(SELECT protocol_mode<>'global' FROM payroll_projection_control WHERE singleton)`, week, requestedGeneration, snapshot,
		calendar, payrollCalculatorVersion, payrollProjectionSchemaVersion, depJSON, driverHeader, investorHeader).Scan(&generation, &resourceProtocol)
	if err != nil {
		return err
	}
	batch := &pgx.Batch{}
	var resourceKeys map[string][]string
	var resourceDependencies map[string]json.RawMessage
	if resourceProtocol {
		resourceKeys, err = payrollPublicationResources(ctx, tx, week.Format(time.DateOnly), reports)
		if err != nil {
			return err
		}
		resourceDependencies, err = payrollPublicationDependencies(ctx, tx, week.Format(time.DateOnly), resourceKeys)
		if err != nil {
			return err
		}
	}
	if selection == nil {
		batch.Queue(`DELETE FROM payroll_statement_state WHERE week_start=$1`, week)
	} else {
		batch.Queue(`DELETE FROM payroll_statement_state WHERE week_start=$1 AND ((kind='driver' AND statement_id=ANY($2::uuid[])) OR (kind='investor' AND statement_id=ANY($3::uuid[])))`, week, selection.drivers, selection.trucks)
	}
	for i, kind := range []string{"driver", "investor"} {
		for _, d := range reports[i].Drivers {
			resources := resourceDependencies[kind+":"+d.ID]
			if !resourceProtocol {
				resources = json.RawMessage(`{}`)
			} else if len(resources) == 0 {
				return ErrPayrollResourceChanged
			}
			evidence := map[string]PayCostTransactions{}
			for _, cost := range []string{"fuel", "toll"} {
				value, e := preparedCostEvidence(ctx, tx, kind, d.ID, week.Format(time.DateOnly), cost, d)
				if e != nil {
					return e
				}
				evidence[cost] = value
			}
			evidenceBody, err := json.Marshal(evidence)
			if err != nil {
				return err
			}
			summary := summarizePayStatement(d)
			fee, err := chargeCents(summary.Summary.Fee)
			if err != nil {
				return err
			}
			payable, err := chargeCents(summary.Summary.Payable)
			if err != nil {
				return err
			}
			body, err := json.Marshal(d)
			if err != nil {
				return err
			}
			short, err := json.Marshal(summary)
			if err != nil {
				return err
			}
			finalized, settlementVersion := false, 0
			if d.Settlement != nil {
				finalized, settlementVersion = d.Settlement.Finalized, d.Settlement.Version
			}
			batch.Queue(`WITH inserted AS (
 INSERT INTO payroll_statement_versions(generation_id,kind,statement_id,week_start,detail,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,cost_evidence,resource_dependencies)
 VALUES($1::uuid,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb) RETURNING id,kind,statement_id,week_start)
 INSERT INTO payroll_statement_state(kind,statement_id,week_start,version_id) SELECT kind,statement_id,week_start,id FROM inserted`,
				generation, kind, d.ID, week, body, short, fee, payable, finalized, d.Edits.Version, settlementVersion, evidenceBody, resources)
		}
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	// The current-row store reuses identical revisions without rewriting them.
	// Their values have nevertheless just been calculated from this generation.
	// Global/dual publication certifies the entire atomic week (the legacy header
	// contract); resource publication certifies only its calculated component.
	var calculatedDrivers, calculatedTrucks []string
	if selection != nil {
		calculatedDrivers, calculatedTrucks = selection.drivers, selection.trucks
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_statement_current c SET generation_id=$1::uuid
 FROM payroll_statement_state s WHERE s.version_id=c.id AND s.week_start=$2 AND c.generation_id<>$1::uuid
 AND ((SELECT protocol_mode<>'resources' FROM payroll_projection_control WHERE singleton) OR $3::boolean
 OR (s.kind='driver' AND s.statement_id=ANY($4::uuid[])) OR (s.kind='investor' AND s.statement_id=ANY($5::uuid[])))`,
		generation, week, selection == nil, calculatedDrivers, calculatedTrucks)
	if err != nil {
		return err
	}
	if payrollConcurrent(tx) {
		if impact, ok := ctx.Value(payrollPublicationImpactKey{}).(*payrollPublicationImpact); ok && impact != nil {
			// Only pre-command versions can receive this transaction's exact
			// revision allowance. Already published rows include those epochs.
			return rebasePayrollStatementResources(ctx, tx, week, generation, snapshot, impact)
		}
		return nil
	}
	if err = rebasePayrollStatementResources(ctx, tx, week, generation, snapshot); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO payroll_projection_weeks(week_start,generation_id) VALUES($1,$2::uuid)
 ON CONFLICT(week_start) DO UPDATE SET generation_id=excluded.generation_id`, week, generation)
	if err != nil {
		return err
	}
	return nil
}

// Process at most one dependency group. All weeks conservatively share source
// dependencies in shadow mode; freshness does not rely on job completion.
func (r *DriverPayRepository) PrepareNextPayrollWeek(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// A board edit may interrupt calculation, but must not discard the result
	// of a just-committed lease claim. Finish this short handoff so cancellation
	// can release that exact token instead of waiting for its lease to expire.
	claimCtx, claimCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	job, err := r.claimPayrollProjection(claimCtx)
	claimCancel()
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err = r.preparePayrollProjection(workCtx, job); err != nil {
		if errors.Is(context.Cause(workCtx), errPayrollWorkYielded) && !errors.Is(err, errPayrollProjectionNeedsFullClaims) {
			err = ErrPayrollProjectionSuperseded
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if releaseErr := r.failPayrollProjection(cleanupCtx, job, err); releaseErr != nil {
			return true, errors.Join(err, releaseErr)
		}
		return true, err
	}
	return true, nil
}
