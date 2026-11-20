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
// the dispatcher’s baseline.
func boardTextOnly(base, edit DriverBoardEntry) bool {
	return base.DriverID == edit.DriverID && base.Version == edit.Version && boardTextCandidate(edit) &&
		base.CurrentLoad == edit.CurrentLoad && base.Status == edit.Status && base.Destination == edit.Destination &&
		base.ETA == edit.ETA && base.HomeTime == "" &&
		leaveValues(base.LeavePlan) == leaveValues(edit.LeavePlan) &&
		(base.Notes != edit.Notes || base.TrailerNumber != edit.TrailerNumber || base.TrailerNote != edit.TrailerNote)
}

func boardTextCandidate(e DriverBoardEntry) bool {
	return e.Version > 0 && !e.DispatchOnly && !e.StatusEdited && !e.ResolveCurrentLoad && e.EffectiveAt == nil &&
		e.StatusReason == "" && e.HomeTime == "" && leaveValues(e.LeavePlan).HomeStart == ""
}

// Apply only the submitted fields to the locked row. Coupled load/status effects
// are calculated by the ordinary save code against this same committed state.
func (e *DriverBoardEntry) applyInlineEdit(ctx context.Context, current DriverBoardEntry) {
	db.Event(ctx, "board_inline_patch")
	if e.Version < current.Version {
		db.Event(ctx, "board_inline_merged")
	}
	edit := *e
	*e = current
	for _, field := range edit.EditedFields {
		switch field {
		case "notes":
			e.Notes = edit.Notes
		case "trailerNumber":
			e.TrailerNumber = edit.TrailerNumber
		case "trailerNote":
			e.TrailerNote = edit.TrailerNote
		case "currentLoad":
			e.CurrentLoad, e.ResolveCurrentLoad = edit.CurrentLoad, edit.ResolveCurrentLoad
		case "status":
			e.Status, e.StatusEdited = edit.Status, edit.StatusEdited
			e.StatusReason, e.EffectiveAt = edit.StatusReason, edit.EffectiveAt
		case "destination":
			e.Destination = edit.Destination
		case "eta":
			e.ETA = edit.ETA
		case "homeTime":
			e.HomeTime = edit.HomeTime
		case "leavePlan":
			e.LeavePlan = edit.LeavePlan
		}
	}
}

func inlineTextEdit(e DriverBoardEntry) bool {
	if e.EditedFields == nil || e.Version <= 0 {
		return false
	}
	for _, field := range e.EditedFields {
		if field != "notes" && field != "trailerNumber" && field != "trailerNote" {
			return false
		}
	}
	return true
}

func leaveValues(plan *DriverLeavePlan) DriverLeavePlan {
	if plan == nil {
		return DriverLeavePlan{}
	}
	return *plan
}

// A rejected save can return the row read under the driver lock. These values
// predate every write in the rejected transaction.
type DriverBoardRowConflict struct {
	Entries []DriverBoardEntry
}

func (*DriverBoardRowConflict) Error() string { return ErrDriverBoardConflict.Error() }
func (*DriverBoardRowConflict) Unwrap() error { return ErrDriverBoardConflict }

var errBoardFullSave = errors.New("board edit requires the full save path")

func (r *DriverBoardRepository) SaveWithTextBase(ctx context.Context, entries []DriverBoardEntry, bases map[string]DriverBoardEntry, actor string) ([]DriverBoardEntry, error) {
	// Older tabs send a complete row without the values the dispatcher saw. A
	// matching persisted version supplies that baseline. Stale legacy rows still
	// go through conflict review. New clients opt out of this probe for full saves.
	if bases == nil && len(entries) == 1 && entries[0].EditedFields == nil && boardTextCandidate(entries[0]) {
		edit := entries[0]
		base, err := scanBoardEntry(r.pool.QueryRow(ctx, boardEntryQuery, edit.DriverID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDriverBoardConflict
		}
		if err != nil {
			return nil, err
		}
		if base.Version != edit.Version {
			return nil, ErrDriverBoardConflict
		}
		bases = map[string]DriverBoardEntry{edit.DriverID: base}
	}
	if len(entries) == 0 {
		return r.saveWithConflict(ctx, entries, actor)
	}
	for _, e := range entries {
		if !inlineTextEdit(e) && (e.EditedFields != nil || len(entries) != len(bases) || !boardTextOnly(bases[e.DriverID], e)) {
			return r.saveWithConflict(ctx, entries, actor)
		}
	}
	saved, err := r.saveText(ctx, entries, bases, actor)
	var pgErr *pgconn.PgError
	if errors.Is(err, errBoardFullSave) || (errors.As(err, &pgErr) && pgErr.Code == "55P03") {
		// The first transaction has rolled back, including any earlier batch rows.
		return r.saveWithConflict(ctx, entries, actor)
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
	batch.Queue(`SELECT id FROM drivers WHERE active AND id=ANY($1::uuid[]) ORDER BY id FOR NO KEY UPDATE`, ids).Query(func(rows pgx.Rows) error {
		locked, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err == nil && !slices.Equal(locked, ids) {
			return ErrDriverBoardConflict
		}
		return err
	})
	currentRows := make([]DriverBoardEntry, len(entries))
	for i, edit := range entries {
		batch.Queue(boardEntryQuery, edit.DriverID).QueryRow(func(row pgx.Row) error {
			entry, readErr := scanBoardEntry(row)
			currentRows[i] = entry
			return readErr
		})
	}
	finish := db.Phase(ctx, "board_text_driver_read")
	err = tx.SendBatch(ctx, &batch).Close()
	finish()
	if err != nil {
		return nil, err
	}
	result := make([]DriverBoardEntry, 0, len(entries))
	for i, edit := range entries {
		current := currentRows[i]
		if current.Version == 0 || current.Version < edit.Version {
			return nil, ErrDriverBoardConflict
		}
		if current.HomeTime != "" || leaveValues(current.LeavePlan).HomeStart != "" {
			return nil, errBoardFullSave
		}
		base := bases[edit.DriverID]
		notes, number, note := current.Notes, current.TrailerNumber, current.TrailerNote
		if edit.EditedFields != nil {
			edit.applyInlineEdit(ctx, current)
			notes, number, note = edit.Notes, edit.TrailerNumber, edit.TrailerNote
		} else {
			if base.Notes != edit.Notes {
				if notes != base.Notes && notes != edit.Notes {
					return nil, &DriverBoardRowConflict{Entries: currentRows}
				}
				notes = edit.Notes
			}
			if base.TrailerNumber != edit.TrailerNumber || base.TrailerNote != edit.TrailerNote {
				matchesBase := number == base.TrailerNumber && note == base.TrailerNote
				matchesEdit := number == edit.TrailerNumber && note == edit.TrailerNote
				if !matchesBase && !matchesEdit {
					return nil, &DriverBoardRowConflict{Entries: currentRows}
				}
				number, note = edit.TrailerNumber, edit.TrailerNote
			}
		}
		if notes != current.Notes || number != current.TrailerNumber || note != current.TrailerNote {
			finish := db.Phase(ctx, "board_text_update")
			err = tx.QueryRow(ctx, `UPDATE driver_board SET notes=$2,trailer_number=$3,trailer_note=$4,
 version=version+1,updated_at=now() WHERE driver_id=$1 AND version=$5 RETURNING version`, edit.DriverID, notes, number, note, current.Version).Scan(&current.Version)
			finish()
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrDriverBoardConflict
			}
			if err != nil {
				return nil, err
			}
			current.Notes, current.TrailerNumber, current.TrailerNote = notes, number, note
		}
		finish := db.Phase(ctx, "board_text_receipt")
		err = attachBoardSaveReceipt(ctx, tx, &current)
		finish()
		if err != nil {
			return nil, err
		}
		result = append(result, current)
	}
	finish = db.Phase(ctx, "board_text_commit")
	err = tx.Commit(ctx)
	finish()
	return result, err
}
