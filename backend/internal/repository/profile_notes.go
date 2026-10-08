package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

var ErrProfileNoteConflict = errors.New("this note ID was already used; reload the notes before adding a new entry")

type ProfileNote struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	ActorName string    `json:"actorName"`
	CreatedAt time.Time `json:"createdAt"`
}

// Only these code-owned column names may be interpolated into note queries.
func profileNoteTarget(truck bool) (string, string) {
	if truck {
		return "trucks", "truck_id"
	}
	return "drivers", "driver_id"
}

func (r *FleetRepository) ProfileNotes(ctx context.Context, id string, truck bool) ([]ProfileNote, error) {
	table, column := profileNoteTarget(truck)
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT id,body,actor_name,created_at FROM profile_notes WHERE `+column+`=$1 ORDER BY created_at DESC,id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ProfileNote{}
	for rows.Next() {
		var n ProfileNote
		if err := rows.Scan(&n.ID, &n.Body, &n.ActorName, &n.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func (r *FleetRepository) AddProfileNote(ctx context.Context, id string, truck bool, noteID, body, actor string) (ProfileNote, error) {
	table, column := profileNoteTarget(truck)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ProfileNote{}, err
	}
	defer tx.Rollback(ctx)
	var existing string
	if err = tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 FOR KEY SHARE`, id).Scan(&existing); err != nil {
		return ProfileNote{}, mapNotFound(err)
	}
	// The client keeps this UUID across retries. Reusing it never rewrites a note.
	if _, err = tx.Exec(ctx, `INSERT INTO profile_notes(id,`+column+`,body,actor_id,actor_name)
 SELECT $1,$2,$3,id,username FROM app_users WHERE id=$4 ON CONFLICT(id) DO NOTHING`, noteID, id, body, actor); err != nil {
		return ProfileNote{}, err
	}
	var n ProfileNote
	err = tx.QueryRow(ctx, `SELECT id,body,actor_name,created_at FROM profile_notes WHERE id=$1 AND `+column+`=$2 AND body=$3 AND actor_id=$4`, noteID, id, body, actor).Scan(&n.ID, &n.Body, &n.ActorName, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProfileNote{}, ErrProfileNoteConflict
	}
	if err != nil {
		return ProfileNote{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ProfileNote{}, err
	}
	return n, nil
}
