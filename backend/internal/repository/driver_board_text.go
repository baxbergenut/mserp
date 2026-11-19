package repository

import (
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/db"
)

// A text edit compares the notes cell or the trailer number/note pair against
// the dispatcher’s baseline. The full submitted row remains usable by older APIs.
func boardTextOnly(base, edit DriverBoardEntry) bool {
	return base.DriverID == edit.DriverID && base.Version > 0 && base.Version == edit.Version &&
		!edit.StatusEdited && !edit.ResolveCurrentLoad && edit.EffectiveAt == nil && edit.StatusReason == "" &&
		base.CurrentLoad == edit.CurrentLoad && base.Status == edit.Status && base.Destination == edit.Destination &&
		base.ETA == edit.ETA && base.HomeTime == "" && edit.HomeTime == "" &&
		leaveValues(base.LeavePlan) == leaveValues(edit.LeavePlan) && leaveValues(base.LeavePlan).HomeStart == "" &&
		(base.Notes != edit.Notes || base.TrailerNumber != edit.TrailerNumber || base.TrailerNote != edit.TrailerNote)
}

func leaveValues(plan *DriverLeavePlan) DriverLeavePlan {
	if plan == nil {
		return DriverLeavePlan{}
	}
	return *plan
}

var errBoardFullSave = errors.New("board edit requires the full save path")

func (r *DriverBoardRepository) SaveWithTextBase(ctx context.Context, entries []DriverBoardEntry, bases map[string]DriverBoardEntry, actor string) ([]DriverBoardEntry, error) {
	if len(bases) != len(entries) || len(entries) == 0 {
		return r.Save(ctx, entries, actor)
	}
	for _, e := range entries {
		if !boardTextOnly(bases[e.DriverID], e) {
			return r.Save(ctx, entries, actor)
		}
	}
	saved, err := r.saveText(ctx, entries, bases, actor)
	var pgErr *pgconn.PgError
	if errors.Is(err, errBoardFullSave) || (errors.As(err, &pgErr) && pgErr.Code == "55P03") {
		// The first transaction has rolled back, including any earlier batch rows.
		return r.Save(ctx, entries, actor)
	}
	return saved, err
}

func (r *DriverBoardRepository) saveText(ctx context.Context, entries []DriverBoardEntry, bases map[string]DriverBoardEntry, actor string) ([]DriverBoardEntry, error) {
	defer db.Phase(ctx, "board_text_save")()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.DriverID
	}
	sort.Strings(ids)
	// All application board commands lock driver rows before board rows. Lock
	// the batch in the same order before comparing against committed values.
	var batch pgx.Batch
	batch.Queue(`SELECT set_config('jit','off',true), set_config('mserp.board_actor',$1,true),
 set_config('mserp.board_source','board',true),set_config('mserp.payroll.board_operational','on',true),
 set_config('mserp.board_text_edit','on',true)`, actor)
	batch.Queue(`SELECT id FROM drivers WHERE active AND id=ANY($1::uuid[]) ORDER BY id FOR UPDATE`, ids).Query(func(rows pgx.Rows) error {
		locked, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err == nil && !slices.Equal(locked, ids) {
			return ErrDriverBoardConflict
		}
		return err
	})
	finish := db.Phase(ctx, "board_text_driver_lock")
	err = tx.SendBatch(ctx, &batch).Close()
	finish()
	if err != nil {
		return nil, err
	}
	result := make([]DriverBoardEntry, 0, len(entries))
	for _, edit := range entries {
		current, err := boardEntry(ctx, tx, edit.DriverID)
		if err != nil {
			return nil, err
		}
		if current.Version == 0 || current.Version < edit.Version {
			return nil, ErrDriverBoardConflict
		}
		if current.HomeTime != "" || leaveValues(current.LeavePlan).HomeStart != "" {
			return nil, errBoardFullSave
		}
		base := bases[edit.DriverID]
		notes, number, note := current.Notes, current.TrailerNumber, current.TrailerNote
		if base.Notes != edit.Notes {
			if notes != base.Notes && notes != edit.Notes {
				return nil, ErrDriverBoardConflict
			}
			notes = edit.Notes
		}
		if base.TrailerNumber != edit.TrailerNumber || base.TrailerNote != edit.TrailerNote {
			matchesBase := number == base.TrailerNumber && note == base.TrailerNote
			matchesEdit := number == edit.TrailerNumber && note == edit.TrailerNote
			if !matchesBase && !matchesEdit {
				return nil, ErrDriverBoardConflict
			}
			number, note = edit.TrailerNumber, edit.TrailerNote
		}
		if notes != current.Notes || number != current.TrailerNumber || note != current.TrailerNote {
			err = tx.QueryRow(ctx, `UPDATE driver_board SET notes=$2,trailer_number=$3,trailer_note=$4,
 version=version+1,updated_at=now() WHERE driver_id=$1 AND version=$5 RETURNING version`, edit.DriverID, notes, number, note, current.Version).Scan(&current.Version)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrDriverBoardConflict
			}
			if err != nil {
				return nil, err
			}
			current.Notes, current.TrailerNumber, current.TrailerNote = notes, number, note
		}
		if err = attachBoardSaveReceipt(ctx, tx, &current); err != nil {
			return nil, err
		}
		result = append(result, current)
	}
	return result, tx.Commit(ctx)
}
