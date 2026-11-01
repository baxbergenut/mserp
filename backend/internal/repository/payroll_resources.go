package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPayrollResourceChanged = fmt.Errorf("%w: payroll resources changed; refresh before saving", ErrDriverPayConflict)

func assertPayrollSelectionResources(ctx context.Context, tx pgx.Tx, selection *payrollSelection) error {
	keys := []string{}
	for kind, ids := range map[string][]string{"driver": selection.drivers, "truck": selection.trucks, "owner": selection.owners} {
		for _, id := range ids {
			keys = append(keys, kind+":"+id)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT payroll_assert_resources($1::text[])`, keys)
	return err
}

// EnterPayrollSourceProtocol must precede a source writer's row, table and
// subsystem locks. Imports and administrative batches retain their own commit
// and recovery boundaries. The schema check preserves older/minimal databases;
// the global compatibility mode leaves their existing locking unchanged.
func EnterPayrollSourceProtocol(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `DO $payroll_source$ DECLARE protocol text; BEGIN
 IF to_regclass(quote_ident(current_schema())||'.payroll_projection_control') IS NOT NULL THEN
  EXECUTE format('SELECT protocol_mode FROM %I.payroll_projection_control WHERE singleton',current_schema()) INTO protocol;
  IF protocol<>'global' THEN PERFORM pg_advisory_xact_lock(736281940); END IF;
 END IF;
 END $payroll_source$`)
	return err
}

func beginPayrollSourceMutation(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = EnterPayrollSourceProtocol(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// A principal lock covers that principal's statements and selection scope across
// weeks, including a currently empty scope. Separate balance keys cover shared
// invoices, escrow accounts and schedules even when no allocation exists yet.
// Discovery is intentionally independent of a calculator's selected week/memos.
type payrollResourceSet struct {
	keys      []string
	selection *payrollSelection
}

func discoverPayrollResources(ctx context.Context, tx pgx.Tx, roots []string) (*payrollResourceSet, error) {
	if len(roots) == 0 {
		return nil, ErrPayrollResourceChanged
	}
	for _, root := range roots {
		kind, id, ok := strings.Cut(root, ":")
		var parsed pgtype.UUID
		if !ok || (kind != "driver" && kind != "truck" && kind != "owner") || parsed.Scan(id) != nil || !parsed.Valid {
			return nil, ErrPayrollResourceChanged
		}
	}
	ctx = context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
	ctx = context.WithValue(ctx, payrollAuthoritativeKey{}, false)
	ctx = context.WithValue(ctx, payrollSelectionFactsKey{}, true)
	selected, err := selectPayrollContexts(payrollWeekContext(ctx, ""), tx, "", roots)
	if err != nil {
		return nil, err
	}
	selection, _ := selected.Value(payrollSelectionKey{}).(*payrollSelection)
	if selection == nil {
		// Coverage uncertainty may use the exclusive domain protocol, never an
		// apparently empty resource claim.
		return nil, ErrPayrollResourceChanged
	}
	return payrollResourcesForSelection(ctx, tx, selection)
}

func payrollResourcesForSelection(ctx context.Context, tx pgx.Tx, selection *payrollSelection) (*payrollResourceSet, error) {
	keys := map[string]bool{}
	for kind, ids := range map[string][]string{"driver": selection.drivers, "truck": selection.trucks, "owner": selection.owners} {
		for _, id := range ids {
			keys[kind+":"+id] = true
		}
	}
	rows, err := tx.Query(ctx, `SELECT 'expense:'||id FROM expenses
 WHERE driver_id=ANY($1::uuid[]) OR charge_driver_id=ANY($1::uuid[]) OR truck_id=ANY($2::uuid[]) OR owner_id=ANY($3::uuid[])
 UNION SELECT 'expense:'||expense_id FROM expense_payments WHERE investor_truck_id=ANY($2::uuid[])
 UNION SELECT 'escrow:'||id FROM driver_escrows WHERE driver_id=ANY($1::uuid[])
 UNION SELECT 'schedule:'||id FROM driver_charge_schedules WHERE driver_id=ANY($1::uuid[])`, selection.drivers, selection.trucks, selection.owners)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys[key] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := &payrollResourceSet{selection: selection, keys: make([]string, 0, len(keys))}
	for key := range keys {
		result.keys = append(result.keys, key)
	}
	sort.Strings(result.keys)
	return result, nil
}

// Call after the discovery transaction ends, while holding the session protocol
// gate. Only then start the financial snapshot and revalidate discovery. Sort
// actual advisory IDs, not resource strings, so hash collisions also obey one
// lock order. Session locks survive the snapshot handoff. A failed acquisition
// closes the connection, releasing even locks acquired by a partially run batch.
func acquirePayrollResources(ctx context.Context, conn *pgxpool.Conn, set *payrollResourceSet) (func(), error) {
	if set == nil || len(set.keys) == 0 {
		return nil, ErrPayrollResourceChanged
	}
	if _, err := conn.Exec(ctx, `SELECT payroll_lock_resources($1::text[])`, set.keys); err != nil {
		_ = conn.Conn().Close(context.Background())
		return nil, err
	}
	return func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(release, `SELECT payroll_unlock_resources($1::text[])`, set.keys); err != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}, nil
}

func revalidatePayrollResources(ctx context.Context, tx pgx.Tx, roots []string, before *payrollResourceSet) error {
	after, err := discoverPayrollResources(ctx, tx, roots)
	if err != nil {
		return err
	}
	if before == nil || !slices.Equal(before.keys, after.keys) {
		return ErrPayrollResourceChanged
	}
	return nil
}

// Settlement/edit metadata is period-local; shared balances keep cross-week
// keys. Legacy overrides without canonical cost receipts additionally depend on
// their original statement because other weeks reconstruct that frozen base.
func payrollStatementResourceSnapshot(ctx context.Context, tx pgx.Tx, kind, id, week string, cache map[string][]string) (json.RawMessage, error) {
	principal := "driver:" + id
	if kind == "investor" {
		principal = "truck:" + id
	}
	keys, known := cache[principal]
	if !known {
		var set *payrollResourceSet
		var err error
		// Lock claims conservatively span history. Freshness dependencies must
		// match this period's actual component: copying the broader claim would
		// mark unrelated current rows stale after an old colleague's save, even
		// though publication correctly selected only its affected period group.
		read := context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
		read = context.WithValue(read, payrollSelectionFactsKey{}, true)
		read, err = selectPayrollContexts(payrollWeekContext(read, week), tx, "", []string{principal})
		if err == nil {
			selection, _ := read.Value(payrollSelectionKey{}).(*payrollSelection)
			if selection == nil {
				return nil, ErrPayrollResourceChanged
			}
			set, err = payrollResourcesForSelection(read, tx, selection)
		}
		if err != nil {
			return nil, err
		}
		keys = append([]string{}, set.keys...)
		rows, err := tx.Query(ctx, `SELECT 'statement:driver:'||w.driver_id||':'||w.week_start FROM driver_pay_weeks w
 WHERE w.driver_id=ANY($1::uuid[]) AND (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start)
 UNION SELECT 'statement:investor:'||w.truck_id||':'||w.week_start FROM investor_pay_weeks w
 WHERE w.truck_id=ANY($2::uuid[]) AND (
 (w.edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel')) OR
 (w.edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll')))
 `, set.selection.drivers, set.selection.trucks)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			if err = rows.Scan(&key); err != nil {
				rows.Close()
				return nil, err
			}
			keys = append(keys, key)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		for _, key := range set.keys {
			if strings.HasPrefix(key, "driver:") || strings.HasPrefix(key, "truck:") || strings.HasPrefix(key, "owner:") {
				cache[key] = keys
			}
		}
	}
	dependencies := append(append([]string{}, keys...), "statement:"+kind+":"+id+":"+week)
	var body json.RawMessage
	err := tx.QueryRow(ctx, `SELECT payroll_resource_snapshot($1::text[])`, dependencies).Scan(&body)
	return body, err
}
