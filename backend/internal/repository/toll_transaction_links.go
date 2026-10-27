package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Attention uses the same coverage assessment as Transactions, before pagination.
func (r *TollRepository) attentionTolls(ctx context.Context, from string, includeReviewed ...bool) ([]Toll, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, selectTollsSQL+` WHERE t.posting_date >= $1::date ORDER BY t.posting_date DESC,t.exit_date DESC,t.exit_time DESC,t.id`, from)
	if err != nil {
		return nil, err
	}
	values := []Toll{}
	ids := []string{}
	for rows.Next() {
		value, e := scanToll(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		values = append(values, value)
		ids = append(ids, value.ID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	flags, err := transactionFlagsTx(ctx, tx, "toll", ids, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	for i := range values {
		if flag := flags[values[i].ID]; flag != nil && flag.Status == "review" {
			values[i].Flag = flag
		}
	}
	if _, err = populateTollReviews(ctx, tx, values); err != nil {
		return nil, err
	}
	out := []Toll{}
	for _, value := range values {
		if value.TruckID == nil || value.Flag != nil && (!value.Reviewed || len(includeReviewed) > 0 && includeReviewed[0]) {
			out = append(out, value)
		}
	}
	return out, tx.Commit(ctx)
}

func validateTollSelection(ids []string) error {
	if len(ids) == 0 || len(ids) > 100 {
		return &TollLinkError{"Select between 1 and 100 transactions."}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		var parsed pgtype.UUID
		if parsed.Scan(id) != nil || !parsed.Valid || seen[id] {
			return &TollLinkError{"Select distinct valid transactions."}
		}
		seen[id] = true
	}
	return nil
}

// Snapshot includes old attribution and all source fields; stale corrections
// cannot move charges to another statement after the operator's preview.
func selectedTollPreview(ctx context.Context, tx pgx.Tx, in PrePassTruckLinkInput) (PrePassTruckPreview, string, error) {
	out := PrePassTruckPreview{Units: []string{}}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks WHERE id=$1)`, in.TruckID).Scan(&exists); err != nil {
		return out, "", err
	}
	if !exists {
		return out, "", &TollLinkError{"Truck no longer exists."}
	}
	var snapshot string
	err := tx.QueryRow(ctx, `SELECT count(*),coalesce(sum(amount),0)::text,coalesce(array_agg(DISTINCT equipment_unit ORDER BY equipment_unit),'{}'),coalesce(jsonb_agg(to_jsonb(t) ORDER BY t.id),'[]')::text FROM tolls t WHERE id=ANY($1::uuid[])`, in.TransactionIDs).Scan(&out.Count, &out.Amount, &out.Units, &snapshot)
	if err != nil {
		return out, "", err
	}
	if out.Count != len(in.TransactionIDs) {
		return out, "", &TollLinkError{"Transactions changed. Select them again."}
	}
	// Guard both the destination and every original truck's finalized statements.
	out.Closed, err = prePassClosed(ctx, tx, in.TransactionIDs, in.TruckID)
	if err != nil {
		return out, "", err
	}
	var oldClosed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tolls t JOIN investor_pay_weeks p ON p.truck_id=t.truck_id AND p.week_start=date_trunc('week',t.posting_date)::date AND p.finalized WHERE t.id=ANY($1::uuid[])) OR EXISTS(SELECT 1 FROM tolls t JOIN truck_driver_assignments a ON a.truck_id=t.truck_id AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=t.exit_date AND (a.unassigned_at IS NULL OR t.exit_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date) JOIN payroll_settlements p ON p.driver_id=a.driver_id AND p.week_start=date_trunc('week',t.posting_date)::date AND p.finalized WHERE t.id=ANY($1::uuid[]))`, in.TransactionIDs).Scan(&oldClosed)
	out.Closed = out.Closed || oldClosed
	ids := append([]string(nil), in.TransactionIDs...)
	sort.Strings(ids)
	raw, _ := json.Marshal(struct {
		Truck string
		IDs   []string
	}{in.TruckID, ids})
	out.Revision = fmt.Sprintf("%x", sha256.Sum256(append(raw, []byte(snapshot)...)))
	return out, snapshot, err
}

func (r *TollRepository) previewSelectedTolls(ctx context.Context, in PrePassTruckLinkInput) (PrePassTruckPreview, error) {
	if err := validateTollSelection(in.TransactionIDs); err != nil {
		return PrePassTruckPreview{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PrePassTruckPreview{}, err
	}
	defer tx.Rollback(ctx)
	out, _, err := selectedTollPreview(ctx, tx, in)
	return out, err
}

func (r *TollRepository) linkSelectedTolls(ctx context.Context, in PrePassTruckLinkInput, actor string) error {
	if err := validateTollSelection(in.TransactionIDs); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(730077)`); err != nil {
		return err
	}
	var identical bool
	err = tx.QueryRow(ctx, `SELECT truck_id=$2::uuid AND revision=$3 FROM toll_transaction_links WHERE id=$1`, in.ID, in.TruckID, in.Revision).Scan(&identical)
	if err == nil {
		if identical {
			return nil
		}
		return &TollLinkError{"This request was already used. Refresh the transactions."}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM tolls WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE`, in.TransactionIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	preview, snapshot, err := selectedTollPreview(ctx, tx, in)
	if err != nil {
		return err
	}
	if preview.Revision != in.Revision {
		return &TollLinkError{"Transactions changed. Preview the selection again."}
	}
	if preview.Closed {
		return &TollLinkError{"Reopen affected statements before linking these tolls."}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO toll_transaction_links(id,truck_id,actor_id,revision,transactions_before) VALUES($1,$2,$3,$4,$5::jsonb)`, in.ID, in.TruckID, actor, in.Revision, snapshot); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tolls SET truck_id=$2,truck_mapping_id=NULL,transaction_link_id=$3 WHERE id=ANY($1::uuid[])`, in.TransactionIDs, in.TruckID, in.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
