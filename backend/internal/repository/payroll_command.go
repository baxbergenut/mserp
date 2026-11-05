package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type PayrollStatementIdentity struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	WeekStart string `json:"weekStart"`
}

type PayrollCommandMetadata struct {
	RequestID                 string `json:"requestId,omitempty"`
	ExpectedStatementRevision string `json:"expectedStatementRevision,omitempty"`
	StatementKind             string `json:"statementKind,omitempty"`
	StatementID               string `json:"statementId,omitempty"`
	StatementWeekStart        string `json:"statementWeekStart,omitempty"`
}

type PayrollSourceCommandReceipt struct {
	RequestID          string                     `json:"requestId"`
	UndoID             string                     `json:"undoId,omitempty"`
	AffectedStatements []PayrollStatementIdentity `json:"affectedStatements"`
}

// Called by an already-authorized source-action route after commit or exact
// replay. Read immutable receipt identities, never newer statement pointers.
func (r *DriverPayRepository) PayrollSourceCommandReceipt(ctx context.Context, actor, requestID string) (PayrollSourceCommandReceipt, error) {
	result := PayrollSourceCommandReceipt{RequestID: requestID}
	var affected []byte
	err := r.pool.QueryRow(ctx, `SELECT CASE WHEN command='source.accept' THEN result#>>'{}' ELSE '' END,affected_statements
 FROM payroll_command_receipts WHERE actor_id=$1::uuid AND request_id=$2::uuid
 AND command IN ('source.accept','source.undo','load.transfer','load.place.driver','load.place.investor')`, actor, requestID).Scan(&result.UndoID, &affected)
	if err == nil {
		err = json.Unmarshal(affected, &result.AffectedStatements)
	}
	return result, err
}

func beginPayrollSourceCommand(ctx context.Context, tx pgx.Tx, actor, name string, meta PayrollCommandMetadata, input any) (*payrollCommand, []byte, error) {
	c, replay, err := beginPayrollCommand(ctx, tx, actor, meta.RequestID, name, meta.ExpectedStatementRevision, input)
	if err != nil || replay != nil {
		return c, replay, err
	}
	if meta.ExpectedStatementRevision != "" {
		if meta.StatementKind != "driver" && meta.StatementKind != "investor" {
			return nil, nil, chargeInvalid("Invalid statement kind")
		}
		var id pgtype.UUID
		if err = id.Scan(meta.StatementID); err != nil || !id.Valid {
			return nil, nil, chargeInvalid("Invalid statement identity")
		}
		if _, err = chargeWeek(meta.StatementWeekStart); err != nil {
			return nil, nil, err
		}
		err = validatePreparedPayrollCommand(ctx, tx, meta.StatementKind, meta.StatementID, meta.StatementWeekStart, meta.ExpectedStatementRevision)
	}
	if err == nil && c != nil && c.expected != "" {
		c.beforeSnapshot, err = payrollCacheSnapshot(ctx, tx)
	}
	if c != nil && c.beforeSnapshot == "" {
		if mutation, ok := tx.(*payrollMutationTransaction); ok {
			c.beforeSnapshot = mutation.beforeSnapshot
		}
	}
	return c, nil, err
}

func commitPayrollSourceCommand(ctx context.Context, tx pgx.Tx, c *payrollCommand, result any) error {
	if c == nil {
		return commitPayrollMutation(ctx, tx)
	}
	var affected []PayrollStatementIdentity
	enabled, _ := ctx.Value(payrollPublicationKey{}).(bool)
	if enabled || c.expected != "" {
		var err error
		impact, impactErr := payrollMutationImpact(ctx, tx, c, "", "")
		if impactErr != nil {
			return impactErr
		}
		affected, err = publishPayrollCommand(ctx, tx, "", impact)
		if err != nil {
			return err
		}
	}
	if err := c.store(ctx, tx, result, affected); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type payrollCommand struct {
	actor, requestID, name, hash, expected string
	before                                 *DriverPayDriver
	beforeSnapshot                         string
	reviewedExpenseRevision                string
}

// Called after authentication/authorization and before version validation. A
// retry recovers the already committed result, never reruns a financial command
// against newer input. Request identity is scoped to its authenticated actor.
func beginPayrollCommand(ctx context.Context, tx pgx.Tx, actor, requestID, name, expected string, input any) (*payrollCommand, []byte, error) {
	if expected != "" {
		var version pgtype.UUID
		if err := version.Scan(expected); err != nil || !version.Valid || requestID == "" {
			return nil, nil, chargeInvalid("Prepared commands require a request identity and valid statement revision")
		}
	}
	if requestID == "" {
		return nil, nil, nil
	}
	if actor == "" {
		return nil, nil, chargeInvalid("An authenticated actor is required")
	}
	var identity pgtype.UUID
	if err := identity.Scan(requestID); err != nil || !identity.Valid {
		return nil, nil, chargeInvalid("Invalid request identity")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(body)
	c := &payrollCommand{actor: actor, requestID: requestID, name: name, hash: hex.EncodeToString(digest[:]), expected: expected}
	// Resource setup already checked absence under its session receipt lock.
	// Reuse only that exact actor/request proof; source commands and replays retain
	// the transaction lock and receipt lookup below.
	if resource, ok := tx.(*payrollResourceTransaction); ok &&
		resource.missingReceiptActor == actor && resource.missingReceiptRequest == requestID {
		if expected != "" {
			if _, err = tx.Exec(ctx, `SELECT payroll_retain_statement_reference($1::uuid)`, expected); err != nil {
				return nil, nil, err
			}
		}
		return c, nil, nil
	}
	// These commands currently hold the financial lock. This independent receipt
	// lock also establishes uniqueness when resource concurrency is enabled later.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,182))`, actor+":"+requestID); err != nil {
		return nil, nil, err
	}
	var oldName, oldHash string
	var result []byte
	err = tx.QueryRow(ctx, `SELECT command,request_hash,result FROM payroll_command_receipts WHERE actor_id=$1::uuid AND request_id=$2::uuid`, actor, requestID).Scan(&oldName, &oldHash, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		if expected != "" {
			if _, err = tx.Exec(ctx, `SELECT payroll_retain_statement_reference($1::uuid)`, expected); err != nil {
				return nil, nil, err
			}
		}
		return c, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if oldName != name || oldHash != c.hash {
		return nil, nil, fmt.Errorf("%w: request ID was already used for different content", ErrDriverPayConflict)
	}
	return c, result, nil
}

func (c *payrollCommand) store(ctx context.Context, tx pgx.Tx, result any, affected []PayrollStatementIdentity) error {
	if c == nil {
		return nil
	}
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if affected == nil {
		affected = []PayrollStatementIdentity{}
	}
	identities, err := json.Marshal(affected)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO payroll_command_receipts(actor_id,request_id,command,request_hash,expected_revision,result,affected_statements)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7)`, c.actor, c.requestID, c.name, c.hash, c.expected, body, identities)
	if err != nil {
		return err
	}
	// Retain the committed versions together, including the last visible version
	// of a statement that disappeared. Removed destinations must remain auditable
	// and must be returned to clients so they can remove their old rows.
	_, err = tx.Exec(ctx, `INSERT INTO payroll_command_versions(actor_id,request_id,version_id)
 SELECT $1::uuid,$2::uuid,coalesce(s.version_id,removed.id)
 FROM jsonb_to_recordset($3::jsonb) a(kind text,id uuid,"weekStart" date)
 LEFT JOIN payroll_statement_state s ON s.kind=a.kind AND s.statement_id=a.id AND s.week_start=a."weekStart"
 LEFT JOIN LATERAL (
 SELECT v.id FROM payroll_statement_versions v JOIN payroll_projection_generations g ON g.id=v.generation_id
 WHERE s.version_id IS NULL AND v.kind=a.kind AND v.statement_id=a.id AND v.week_start=a."weekStart"
 ORDER BY g.prepared_at DESC,g.id DESC LIMIT 1
 ) removed ON true WHERE coalesce(s.version_id,removed.id) IS NOT NULL ON CONFLICT DO NOTHING`, c.actor, c.requestID, identities)
	return err
}

// This conservative publication path is the correctness fallback. The caller's
// canonical writes, every materialized dependent week, and receipt commit as one
// transaction. Incremental publication may narrow this only with complete impact
// evidence; the financial lock remains until that protocol is validated.
func publishPayrollCommand(ctx context.Context, tx pgx.Tx, week string, impacts ...*payrollPublicationImpact) ([]PayrollStatementIdentity, error) {
	concurrent := payrollConcurrent(tx)
	if concurrent && (len(impacts) == 0 || impacts[0] == nil || impacts[0].refreshHeader) {
		return nil, ErrPayrollResourceChanged
	}
	if err := payrollProjectionCoverage(ctx, tx); err != nil {
		return nil, err
	}
	// Revision fences are established in the same transaction as the financial
	// writes. A source commit against the pinned snapshot aborts this command.
	rows, err := tx.Query(ctx, `SELECT table_name,revision FROM payroll_cache_revisions WHERE NOT table_name=ANY($1::text[]) ORDER BY table_name FOR SHARE`, payrollUnrelatedTables)
	if err != nil {
		return nil, err
	}
	dependencies := map[string]int64{}
	for rows.Next() {
		var table string
		var revision int64
		if err = rows.Scan(&table, &revision); err != nil {
			rows.Close()
			return nil, err
		}
		dependencies[table] = revision
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Drop every read-phase memo after writes, including caller selection roots.
	validatedSelection, _ := ctx.Value(payrollSelectionKey{}).(*payrollSelection)
	validatedWeek := payrollContextWeek(ctx)
	ctx = context.WithValue(ctx, payrollReadKey{}, (*payrollReadState)(nil))
	ctx = context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
	ctx = context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{})
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return nil, err
	}
	// Canonical writes are complete. Everything below reads this fenced input
	// phase and writes derived tables only, so all calculators share its revision.
	ctx = context.WithValue(ctx, payrollSnapshotKey{}, payrollSnapshot{tx, snapshot})
	ctx = context.WithValue(ctx, payrollFinancialSnapshotKey{}, payrollSnapshot{})
	financial, err := payrollFinancialSnapshot(ctx, tx)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, payrollFinancialSnapshotKey{}, payrollSnapshot{tx, financial})
	ctx = payrollRoutingFactsContext(ctx, snapshot)
	ctx = payrollPublicationReadContext(ctx, tx)
	if len(impacts) == 0 || impacts[0] == nil {
		if err = preparePayrollLoadFacts(ctx, tx); err != nil {
			return nil, err
		}
	} else if len(impacts[0].loadIDs) > 0 {
		if err = preparePayrollLoadFacts(ctx, tx, impacts[0].loadIDs); err != nil {
			return nil, err
		}
	}
	ctx = context.WithValue(ctx, payrollNormalizedFactsKey{}, true)
	rows, err = tx.Query(ctx, `SELECT periods.week_start,coalesce(CASE WHEN g.calendar_week=$2::date AND g.calculator_version=$3 AND g.schema_version=$4 THEN g.input_snapshot END,'')
 FROM (SELECT week_start FROM payroll_projection_weeks UNION SELECT nullif($1,'')::date WHERE $1<>'') periods
 LEFT JOIN payroll_projection_weeks w ON w.week_start=periods.week_start
 LEFT JOIN payroll_projection_generations g ON g.id=w.generation_id
 WHERE periods.week_start BETWEEN (SELECT coalesce(first_week,'-infinity'::date) FROM payroll_projection_control WHERE singleton) AND $5::date ORDER BY periods.week_start`, week, ChargeCurrentWeek(), payrollCalculatorVersion, payrollProjectionSchemaVersion, PayrollLatestWeek())
	if err != nil {
		return nil, err
	}
	weeks := []time.Time{}
	previousSnapshots := map[string]string{}
	for rows.Next() {
		var w time.Time
		var previous string
		if err = rows.Scan(&w, &previous); err != nil {
			rows.Close()
			return nil, err
		}
		weeks = append(weeks, w)
		previousSnapshots[w.Format(time.DateOnly)] = previous
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	affectedSet := map[PayrollStatementIdentity]bool{}
	selectionWeeks := []string{}
	eligibleSnapshots := map[string]bool{}
	eligiblePeriods := map[string]bool{}
	if len(impacts) > 0 && impacts[0] != nil {
		eligibleSnapshots[impacts[0].snapshot] = true
		for _, w := range weeks {
			date := w.Format(time.DateOnly)
			previous := previousSnapshots[date]
			if _, checked := eligibleSnapshots[previous]; concurrent && !checked {
				eligibleSnapshots[previous], err = payrollSourcesUnrelated(ctx, tx, previous, snapshot)
				if err != nil {
					return nil, err
				}
			}
			eligiblePeriods[date] = eligibleSnapshots[previous]
			if resource, ok := tx.(*payrollResourceTransaction); ok && resource.periodSources[date+":"+previous] {
				eligiblePeriods[date] = true
			}
			if eligiblePeriods[date] && (impacts[0].weeks == nil || impacts[0].weeks[date]) {
				selectionWeeks = append(selectionWeeks, date)
			}
		}
	}
	ctx, err = payrollSelectionBatchContext(ctx, tx, selectionWeeks)
	if err != nil {
		return nil, err
	}
	type periodRead struct {
		ctx        context.Context
		week       time.Time
		rebaseOnly bool
	}
	periods := []periodRead{}
	scopes := []payrollBalanceScope{}
	truckScopes := []payrollTruckInputScope{}
	for _, w := range weeks {
		readCtx := ctx
		rebaseOnly := false
		if len(impacts) > 0 && impacts[0] != nil {
			if eligiblePeriods[w.Format(time.DateOnly)] {
				rebaseOnly = impacts[0].weeks != nil && !impacts[0].weeks[w.Format(time.DateOnly)]
				if rebaseOnly {
					readCtx = context.WithValue(readCtx, payrollSelectionKey{}, &payrollSelection{drivers: []string{}, trucks: []string{}, owners: []string{}})
				} else if impacts[0].weeks != nil && validatedWeek == w.Format(time.DateOnly) && payrollSelectionCovers(validatedSelection, impacts[0].roots) {
					// The impact proof excludes allocations, relationship edits and
					// legacy cost bases whose graph changes with finalization. That
					// metadata-only command retains its already validated component.
					readCtx = context.WithValue(readCtx, payrollSelectionKey{}, validatedSelection)
				} else {
					readCtx, err = selectPayrollContexts(payrollWeekContext(readCtx, w.Format(time.DateOnly)), tx, "", impacts[0].roots)
					if err != nil {
						return nil, err
					}
				}
				selection, _ := readCtx.Value(payrollSelectionKey{}).(*payrollSelection)
				readCtx = context.WithValue(readCtx, payrollPublicationSelectionKey{}, selection)
				readCtx = context.WithValue(readCtx, payrollPublicationHeaderKey{}, impacts[0].refreshHeader && !rebaseOnly)
			} else if concurrent {
				return nil, ErrDriverPayConflict
			}
		}
		if concurrent && rebaseOnly {
			continue
		}
		periods = append(periods, periodRead{readCtx, w, rebaseOnly})
		if ids := payrollTruckIDs(readCtx); !rebaseOnly && ids != nil {
			truckScopes = append(truckScopes, payrollTruckInputScope{Week: w.Format(time.DateOnly), Trucks: ids})
		}
		if ids := payrollDriverIDs(readCtx); !rebaseOnly && ids != nil {
			scopes = append(scopes, payrollBalanceScope{Week: w.Format(time.DateOnly), Drivers: ids})
		}
	}
	if len(scopes) > 1 {
		ctx, err = payrollBalanceBatchContext(ctx, tx, scopes)
		if err != nil {
			return nil, err
		}
		if err = preparePayrollDriverEligibility(ctx, tx, scopes); err != nil {
			return nil, err
		}
	}
	if err = preparePayrollTruckInputs(ctx, tx, truckScopes); err != nil {
		return nil, err
	}
	calculatedWeeks := []string{}
	calculatedDates := []time.Time{}
	for _, period := range periods {
		if !period.rebaseOnly {
			calculatedWeeks = append(calculatedWeeks, period.week.Format(time.DateOnly))
			calculatedDates = append(calculatedDates, period.week)
		}
	}
	if err = prefetchPayrollRoutingFacts(ctx, tx, calculatedDates); err != nil {
		return nil, err
	}
	if err = preparePayrollScaleWeeks(ctx, tx, calculatedWeeks); err != nil {
		return nil, err
	}
	if concurrent {
		// All period scopes were discovered under the same source fence. Assert
		// their union once before publishing; every period must still be explicit.
		claimed := map[string]bool{}
		for _, period := range periods {
			selection, _ := period.ctx.Value(payrollPublicationSelectionKey{}).(*payrollSelection)
			if selection == nil {
				return nil, ErrPayrollResourceChanged
			}
			for kind, ids := range map[string][]string{"driver": selection.drivers, "truck": selection.trucks, "owner": selection.owners} {
				for _, id := range ids {
					claimed[kind+":"+id] = true
				}
			}
		}
		keys := make([]string, 0, len(claimed))
		for key := range claimed {
			keys = append(keys, key)
		}
		if _, err = tx.Exec(ctx, `SELECT payroll_assert_resources($1::text[])`, keys); err != nil {
			return nil, err
		}
	}
	for _, period := range periods {
		readCtx, w, rebaseOnly := period.ctx, period.week, period.rebaseOnly
		if len(impacts) > 0 && impacts[0] != nil {
			readCtx = context.WithValue(readCtx, payrollPublicationImpactKey{}, impacts[0])
		}
		if batch, ok := ctx.Value(payrollBalanceBatchKey{}).(*payrollBalanceBatch); ok {
			readCtx = context.WithValue(readCtx, payrollBalanceBatchKey{}, batch)
		}
		var reports [2]DriverPayWeek
		var e error
		finalizedKind := ""
		publicationSelection, _ := readCtx.Value(payrollPublicationSelectionKey{}).(*payrollSelection)
		if len(impacts) > 0 && impacts[0] != nil && !rebaseOnly && publicationSelection != nil {
			finalizedKind = impacts[0].finalizedKind
		}
		if rebaseOnly {
			for i, kind := range []string{"driver", "investor"} {
				reports[i].WeekStart = w.Format(time.DateOnly)
				if e = setPayrollRevision(readCtx, tx, kind, &reports[i]); e != nil {
					return nil, e
				}
			}
		} else if finalizedKind == "driver" || finalizedKind == "investor" {
			// Captured events prove that freeze/reopen changed no non-live
			// allocation or source, and no legacy cost depends on the frozen base.
			// Preserve the other page's immutable versions and resource epochs.
			index := 0
			if finalizedKind == "investor" {
				index = 1
			}
			var reused bool
			reports[index], reused, e = payrollPostWriteReport(readCtx, tx, finalizedKind, w)
			if e == nil && !reused {
				if index == 0 {
					reports[index], e = readDriverPayWeek(readCtx, tx, w, "")
				} else {
					reports[index], e = readInvestorPay(readCtx, tx, w)
				}
			}
			if e == nil {
				e = decoratePayHomeWeeks(readCtx, tx, finalizedKind, &reports[index])
			}
			reports[1-index].WeekStart = w.Format(time.DateOnly)
			if e == nil {
				e = setPayrollRevision(readCtx, tx, []string{"investor", "driver"}[index], &reports[1-index])
			}
			if selection, ok := readCtx.Value(payrollPublicationSelectionKey{}).(*payrollSelection); ok && selection != nil {
				only := *selection
				if index == 0 {
					only.trucks = []string{}
				} else {
					only.drivers = []string{}
				}
				readCtx = context.WithValue(readCtx, payrollPublicationSelectionKey{}, &only)
			}
		} else {
			reports, e = readPayrollProjectionReference(readCtx, tx, w, false)
		}
		if e != nil {
			return nil, e
		}
		// Include old destinations before replacing state. A zero balance, changed
		// eligibility or moved source can remove a statement from the new report.
		selection, _ := readCtx.Value(payrollPublicationSelectionKey{}).(*payrollSelection)
		var drivers, trucks []string
		if selection != nil {
			drivers, trucks = selection.drivers, selection.trucks
		}
		previousVersions := map[PayrollStatementIdentity]string{}
		old, e := tx.Query(ctx, `SELECT kind,statement_id::text,version_id::text FROM payroll_statement_state WHERE week_start=$1
 AND ($2::boolean OR (kind='driver' AND statement_id=ANY($3::uuid[])) OR (kind='investor' AND statement_id=ANY($4::uuid[])))`, w, selection == nil, drivers, trucks)
		if e != nil {
			return nil, e
		}
		for old.Next() {
			identity := PayrollStatementIdentity{WeekStart: w.Format(time.DateOnly)}
			var version string
			if e = old.Scan(&identity.Kind, &identity.ID, &version); e != nil {
				old.Close()
				return nil, e
			}
			affectedSet[identity] = true
			previousVersions[identity] = version
		}
		old.Close()
		if e = old.Err(); e != nil {
			return nil, e
		}
		var generation int64
		if !concurrent {
			e = tx.QueryRow(ctx, `INSERT INTO payroll_projection_jobs(week_start) VALUES($1)
 ON CONFLICT(week_start) DO UPDATE SET requested_generation=payroll_projection_jobs.requested_generation+1
 RETURNING requested_generation`, w).Scan(&generation)
			if e != nil {
				return nil, e
			}
		}
		if e = writePayrollProjection(readCtx, tx, w, generation, snapshot, ChargeCurrentWeek(), dependencies, reports); e != nil {
			return nil, e
		}
		if !concurrent {
			_, e = tx.Exec(ctx, `UPDATE payroll_projection_jobs SET completed_generation=requested_generation,lease_token=NULL,lease_until=NULL,last_error='',attempts=0 WHERE week_start=$1`, w)
			if e != nil {
				return nil, e
			}
		}
		// Unchanged publication reuses its current revision. Receipts enumerate
		// only real changes and removals, rather than claiming no-op copies.
		published, e := tx.Query(ctx, `SELECT kind,statement_id::text,version_id::text FROM payroll_statement_state WHERE week_start=$1
 AND ($2::boolean OR (kind='driver' AND statement_id=ANY($3::uuid[])) OR (kind='investor' AND statement_id=ANY($4::uuid[])))`, w, selection == nil, drivers, trucks)
		if e != nil {
			return nil, e
		}
		for published.Next() {
			identity := PayrollStatementIdentity{WeekStart: w.Format(time.DateOnly)}
			var version string
			if e = published.Scan(&identity.Kind, &identity.ID, &version); e != nil {
				published.Close()
				return nil, e
			}
			if previousVersions[identity] == version {
				delete(affectedSet, identity)
			} else {
				affectedSet[identity] = true
			}
		}
		published.Close()
		if e = published.Err(); e != nil {
			return nil, e
		}
	}
	// These exact events belong to this command and all their materialized
	// destinations have now been published. Later transactions remain pending.
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_changes SET dispatched_at=clock_timestamp() WHERE source_xid=pg_current_xact_id() AND dispatched_at IS NULL`)
	affected := make([]PayrollStatementIdentity, 0, len(affectedSet))
	for identity := range affectedSet {
		affected = append(affected, identity)
	}
	sort.Slice(affected, func(i, j int) bool {
		a, b := affected[i], affected[j]
		if a.WeekStart != b.WeekStart {
			return a.WeekStart < b.WeekStart
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	return affected, err
}

type payrollPublicationKey struct{}

type payrollMutationTransaction struct {
	pgx.Tx
	beforeSnapshot string
}

func WithPayrollPublication(ctx context.Context) context.Context {
	return context.WithValue(ctx, payrollPublicationKey{}, true)
}

// Native financial edits join the same publication boundary. Provider workers
// do not carry this request context and retain durable asynchronous preparation.
func beginPayrollMutation(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if enabled, _ := ctx.Value(payrollPublicationKey{}).(bool); enabled {
		if err = lockTruckAccounting(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		snapshot, snapshotErr := payrollCacheSnapshot(ctx, tx)
		if snapshotErr != nil {
			_ = tx.Rollback(ctx)
			return nil, snapshotErr
		}
		return &payrollMutationTransaction{Tx: tx, beforeSnapshot: snapshot}, nil
	}
	if err = EnterPayrollSourceProtocol(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func commitPayrollMutation(ctx context.Context, tx pgx.Tx) error {
	if enabled, _ := ctx.Value(payrollPublicationKey{}).(bool); enabled {
		var changed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_projection_changes WHERE source_xid=pg_current_xact_id_if_assigned() AND dispatched_at IS NULL)`).Scan(&changed); err != nil {
			return err
		}
		if changed {
			handled, err := publishLiveDriverExpenses(ctx, tx)
			if err != nil {
				return err
			}
			if handled {
				return tx.Commit(ctx)
			}
			var impact *payrollPublicationImpact
			if mutation, ok := tx.(*payrollMutationTransaction); ok {
				var err error
				impact, err = payrollMutationImpact(ctx, tx, &payrollCommand{name: "native", beforeSnapshot: mutation.beforeSnapshot}, "", "")
				if err != nil {
					return err
				}
			}
			if _, err := publishPayrollCommand(ctx, tx, "", impact); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// Transport metadata is never part of an editable ledger row or its audit body.
// Preserve it only on the immutable request receipt and outgoing response.
func stripPayCommandMetadata(edits *DriverPayEdits) {
	edits.RequestID = ""
	edits.ExpectedStatementRevision = ""
	edits.StatementRevision = ""
	edits.CommittedStatement = nil
	edits.AffectedStatements = nil
}

func preparedSaveCard(ctx context.Context, tx pgx.Tx, kind, id, week string, c *payrollCommand) (DriverPayDriver, error) {
	var d DriverPayDriver
	var body []byte
	err := tx.QueryRow(ctx, `SELECT v.detail,g.input_snapshot FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id
 JOIN payroll_projection_weeks w ON w.week_start=s.week_start JOIN payroll_projection_generations g ON g.id=w.generation_id
 WHERE s.kind=$1 AND s.statement_id=$2::uuid AND s.week_start=$3::date AND s.version_id=$4::uuid`, kind, id, week, c.expected).Scan(&body, &c.beforeSnapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrDriverPayConflict
	}
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(body, &d)
	if err == nil && kind == "driver" {
		report := DriverPayWeek{WeekStart: week, Drivers: []DriverPayDriver{d}}
		err = overlayDriverExpenseBalances(ctx, tx, &report)
		if err == nil {
			d = report.Drivers[0]
		}
	}
	if err == nil {
		c.before = &d
	}
	return d, err
}

func validatePreparedPayrollCommand(ctx context.Context, tx pgx.Tx, kind, id, week, expected string) error {
	if expected == "" {
		return nil
	}
	// Share capture proofs only during this read-only validation phase. The
	// context never escapes into posting, so own writes cannot reuse an old proof.
	ctx = context.WithValue(ctx, payrollSourceImpactMemoKey{}, &payrollSourceImpactMemo{
		tx: tx, proofs: map[[3]string]*payrollPublicationImpact{},
	})
	if err := payrollProjectionCoverage(ctx, tx); err != nil {
		return err
	}
	var revision, snapshot, calendar, calculator string
	var schema int
	var currentResources bool
	err := tx.QueryRow(ctx, `SELECT s.version_id::text,g.input_snapshot,g.calendar_week::text,g.calculator_version,g.schema_version,payroll_resources_current(v.resource_dependencies)
 FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id
 JOIN payroll_projection_weeks w ON w.week_start=s.week_start
 JOIN payroll_projection_generations g ON g.id=CASE WHEN (SELECT protocol_mode='resources' FROM payroll_projection_control WHERE singleton)
 THEN v.generation_id ELSE w.generation_id END
 WHERE s.kind=$1 AND s.statement_id=$2::uuid AND s.week_start=$3::date`, kind, id, week).Scan(&revision, &snapshot, &calendar, &calculator, &schema, &currentResources)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDriverPayConflict
	}
	if err != nil {
		return err
	}
	current, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return err
	}
	if revision != expected || calendar != ChargeCurrentWeek() || calculator != payrollCalculatorVersion || schema != payrollProjectionSchemaVersion {
		return ErrDriverPayConflict
	}
	if snapshot != current || !currentResources {
		unrelated, err := preparedPayrollStatementCurrent(ctx, tx, kind, id, week, expected, current)
		if err != nil {
			return err
		}
		if !unrelated {
			return ErrDriverPayConflict
		}
	}
	return retainPayrollPeriodSourceProofs(ctx, tx, current)
}

func finishPaySaveCommand(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind string, edits *DriverPayEdits, publish bool) error {
	if publish || (c != nil && c.expected != "") {
		affected, handled, err := publishPayrollTextSave(ctx, tx, c, kind, *edits)
		if err == nil && !handled {
			var impact *payrollPublicationImpact
			impact, err = payrollMutationImpact(ctx, tx, c, kind, edits.DriverID)
			if err == nil {
				affected, err = publishPayrollCommand(ctx, tx, edits.WeekStart, impact)
			}
		}
		if err != nil {
			return err
		}
		var body []byte
		err = tx.QueryRow(ctx, `SELECT s.version_id::text,v.detail FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id
 WHERE s.kind=$1 AND s.statement_id=$2::uuid AND s.week_start=$3::date`, kind, edits.DriverID, edits.WeekStart).Scan(&edits.StatementRevision, &body)
		if err != nil {
			return err
		}
		var d DriverPayDriver
		if err = json.Unmarshal(body, &d); err != nil {
			return err
		}
		edits.AffectedStatements = affected
		edits.CommittedStatement = &d
		edits.ExpectedStatementRevision = edits.StatementRevision
	}
	return c.store(ctx, tx, edits, edits.AffectedStatements)
}

type PayrollSettlementCommand struct{ RequestID, ExpectedStatementRevision, ExpenseRevision string }

func beginPayrollSettlement(ctx context.Context, tx pgx.Tx, kind string, week time.Time, id, revision, actor, reason string, reopen, override bool, options []PayrollSettlementCommand) (*payrollCommand, []byte, error) {
	if len(options) == 0 {
		return nil, nil, nil
	}
	opt := options[0]
	name := kind + ".finalize"
	if reopen {
		name = kind + ".reopen"
	}
	input := PayrollSettlementInput{RequestID: opt.RequestID, ExpectedStatementRevision: opt.ExpectedStatementRevision, ExpenseRevision: opt.ExpenseRevision, WeekStart: week.Format(time.DateOnly), DriverID: id, Revision: revision, Reason: reason, Override: override}
	c, replay, err := beginPayrollCommand(ctx, tx, actor, opt.RequestID, name, opt.ExpectedStatementRevision, input)
	if err != nil || replay != nil {
		return c, replay, err
	}
	if id != "" {
		err = validatePreparedPayrollCommand(ctx, tx, kind, id, input.WeekStart, opt.ExpectedStatementRevision)
	} else if opt.ExpectedStatementRevision != "" {
		if err = payrollProjectionCoverage(ctx, tx); err != nil {
			return nil, nil, err
		}
		var generation, snapshot, calendar, calculator string
		var schema int
		err = tx.QueryRow(ctx, `SELECT payroll_week_revision(w.week_start,$2,g.id),g.input_snapshot,g.calendar_week::text,g.calculator_version,g.schema_version FROM payroll_projection_weeks w
JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1`, week, kind).Scan(&generation, &snapshot, &calendar, &calculator, &schema)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrDriverPayConflict
		}
		if err == nil {
			var current string
			current, err = payrollCacheSnapshot(ctx, tx)
			if err == nil && (generation != opt.ExpectedStatementRevision || snapshot != current || calendar != ChargeCurrentWeek() || calculator != payrollCalculatorVersion || schema != payrollProjectionSchemaVersion) {
				err = ErrDriverPayConflict
			}
		}
	}
	if err == nil && c != nil && c.expected != "" {
		if id != "" {
			// Validation may have proved that newer captured sources are outside
			// this locked component. Retain the reviewed header's actual fence;
			// never certify the rest of that stale week as current.
			err = tx.QueryRow(ctx, `SELECT g.input_snapshot FROM payroll_projection_weeks w
 JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1`, week).Scan(&c.beforeSnapshot)
		} else {
			c.beforeSnapshot, err = payrollCacheSnapshot(ctx, tx)
		}
	}
	return c, nil, err
}

func (r *DriverPayRepository) commitPayrollSettlement(ctx context.Context, tx pgx.Tx, c *payrollCommand, kind string, result DriverPayWeek, selected map[string]bool) (DriverPayWeek, error) {
	if r.preparedReads || (c != nil && c.expected != "") {
		impact, err := payrollMutationImpact(ctx, tx, c, kind, "")
		if err != nil {
			return DriverPayWeek{}, err
		}
		var affected []PayrollStatementIdentity
		published := false
		if r.preparedFinalization {
			affected, published, err = publishPayrollFrozen(ctx, tx, c, impact, kind, result, selected)
		}
		if err == nil && !published {
			affected, err = publishPayrollCommand(ctx, tx, result.WeekStart, impact)
		}
		if err != nil {
			return DriverPayWeek{}, err
		}
		ids := make([]string, 0, len(result.Drivers))
		for _, d := range result.Drivers {
			ids = append(ids, d.ID)
		}
		rows, err := tx.Query(ctx, `SELECT s.version_id::text,v.detail FROM payroll_statement_state s JOIN payroll_statement_versions v ON v.id=s.version_id WHERE s.kind=$1 AND s.week_start=$2::date AND s.statement_id=ANY($3::uuid[])`, kind, result.WeekStart, ids)
		if err != nil {
			return DriverPayWeek{}, err
		}
		current := map[string]DriverPayDriver{}
		for rows.Next() {
			var revision string
			var body []byte
			if err = rows.Scan(&revision, &body); err != nil {
				rows.Close()
				return DriverPayWeek{}, err
			}
			var d DriverPayDriver
			if err = json.Unmarshal(body, &d); err != nil {
				rows.Close()
				return DriverPayWeek{}, err
			}
			d.StatementRevision = revision
			d.Edits.ExpectedStatementRevision = revision
			current[d.ID] = d
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return DriverPayWeek{}, err
		}
		for i, d := range result.Drivers {
			if fresh, ok := current[d.ID]; ok {
				result.Drivers[i] = fresh
			} else {
				return DriverPayWeek{}, ErrDriverPayConflict
			}
		}
		result.AffectedStatements = affected
	}
	if err := c.store(ctx, tx, result, result.AffectedStatements); err != nil {
		return DriverPayWeek{}, err
	}
	return result, tx.Commit(ctx)
}
