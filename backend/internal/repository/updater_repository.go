package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var ErrUpdaterConflict = errors.New("updater changed or is assigned to dispatchers; reload, and remove assignments before changing shift")

type Updater struct {
	ID              string   `json:"id"`
	FullName        string   `json:"fullName"`
	Shift           string   `json:"shift"`
	Extension       *int     `json:"extension"`
	Version         int      `json:"version"`
	DispatcherNames []string `json:"dispatcherNames"`
}
type UpdaterInput struct {
	FullName  string `json:"fullName"`
	Shift     string `json:"shift"`
	Extension *int   `json:"extension"`
	Version   int    `json:"version"`
}
type UpdaterAssignments struct {
	MainUpdaterID       *string `json:"mainUpdaterId"`
	AfterHoursUpdaterID *string `json:"afterHoursUpdaterId"`
}

const updaterSelect = `SELECT u.id,u.full_name,u.shift,u.extension,u.version,
 ARRAY(SELECT d.full_name FROM dispatcher_updaters a JOIN dispatchers d ON d.id=a.dispatcher_id WHERE a.updater_id=u.id ORDER BY d.full_name)
 FROM updaters u`

func scanUpdater(row rowScanner) (Updater, error) {
	var u Updater
	err := row.Scan(&u.ID, &u.FullName, &u.Shift, &u.Extension, &u.Version, &u.DispatcherNames)
	return u, err
}
func (r *FleetRepository) ListUpdaters(ctx context.Context) ([]Updater, error) {
	rows, err := r.pool.Query(ctx, updaterSelect+` ORDER BY u.full_name,u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Updater{}
	for rows.Next() {
		u, err := scanUpdater(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
func (r *FleetRepository) SaveUpdater(ctx context.Context, id string, in UpdaterInput) (Updater, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Updater{}, err
	}
	defer tx.Rollback(ctx)
	name := formatPersonName(in.FullName)
	if id == "" {
		err = tx.QueryRow(ctx, `INSERT INTO updaters(full_name,normalized_name,shift,extension) VALUES($1,$2,$3,$4) RETURNING id`, name, normalizeName(name), in.Shift, in.Extension).Scan(&id)
	} else {
		var shift string
		var version int
		err = tx.QueryRow(ctx, `SELECT shift,version FROM updaters WHERE id=$1 FOR UPDATE`, id).Scan(&shift, &version)
		if err != nil {
			return Updater{}, mapNotFound(err)
		}
		if version != in.Version {
			return Updater{}, ErrUpdaterConflict
		}
		if shift != in.Shift {
			var assigned bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dispatcher_updaters WHERE updater_id=$1)`, id).Scan(&assigned)
			if err != nil {
				return Updater{}, err
			}
			if assigned {
				return Updater{}, ErrUpdaterConflict
			}
		}
		_, err = tx.Exec(ctx, `UPDATE updaters SET full_name=$2,normalized_name=$3,shift=$4,extension=$5,version=version+1 WHERE id=$1`, id, name, normalizeName(name), in.Shift, in.Extension)
	}
	if err != nil {
		return Updater{}, err
	}
	u, err := scanUpdater(tx.QueryRow(ctx, updaterSelect+` WHERE u.id=$1`, id))
	if err != nil {
		return u, err
	}
	return u, tx.Commit(ctx)
}
func (r *FleetRepository) DeleteUpdater(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM updaters WHERE id=$1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func setDispatcherUpdaters(ctx context.Context, tx pgx.Tx, id string, in *UpdaterAssignments) error {
	if in == nil {
		return nil
	} // Older clients preserve assignments.
	if _, err := tx.Exec(ctx, `DELETE FROM dispatcher_updaters WHERE dispatcher_id=$1`, id); err != nil {
		return err
	}
	for _, a := range []struct {
		shift string
		id    *string
	}{{"main", in.MainUpdaterID}, {"after_hours", in.AfterHoursUpdaterID}} {
		if a.id != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO dispatcher_updaters(dispatcher_id,shift,updater_id) VALUES($1,$2,$3)`, id, a.shift, *a.id); err != nil {
				return err
			}
		}
	}
	return nil
}
