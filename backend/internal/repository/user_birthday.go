package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type BirthdayGreeting struct {
	Celebrate   bool       `json:"celebrate"`
	Name        string     `json:"name,omitempty"`
	CountdownAt *time.Time `json:"countdownAt,omitempty"`
	ServerNow   time.Time  `json:"serverNow"`
}

// The atomic claim prevents repeats across sessions, tabs and devices. It only
// returns a greeting, never DOB, age or phone. February 29 matches leap years only.
func (r *AuthRepository) ClaimBirthday(ctx context.Context, userID string, day time.Time) (BirthdayGreeting, error) {
	tomorrow := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, day.Location())
	var today, next bool
	err := r.pool.QueryRow(ctx, `SELECT
 extract(month FROM date_of_birth)=$2 AND extract(day FROM date_of_birth)=$3 AND birthday_greeted_year IS DISTINCT FROM $4,
 extract(month FROM date_of_birth)=$5 AND extract(day FROM date_of_birth)=$6 AND birthday_greeted_year IS DISTINCT FROM $7
 FROM app_users WHERE id=$1 AND active AND date_of_birth IS NOT NULL`,
		userID, int(day.Month()), day.Day(), day.Year(), int(tomorrow.Month()), tomorrow.Day(), tomorrow.Year()).Scan(&today, &next)
	if errors.Is(err, pgx.ErrNoRows) {
		return BirthdayGreeting{}, nil
	}
	if err != nil {
		return BirthdayGreeting{}, err
	}
	if next {
		return BirthdayGreeting{CountdownAt: &tomorrow}, nil
	}
	if !today {
		return BirthdayGreeting{}, nil
	}
	var name string
	err = r.pool.QueryRow(ctx, `UPDATE app_users SET birthday_greeted_year=$2
 WHERE id=$1 AND active AND extract(month FROM date_of_birth)=$3
 AND extract(day FROM date_of_birth)=$4 AND birthday_greeted_year IS DISTINCT FROM $2
 RETURNING coalesce(nullif(first_name,''),username)`, userID, day.Year(), int(day.Month()), day.Day()).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return BirthdayGreeting{}, nil
	}
	if err != nil {
		return BirthdayGreeting{}, err
	}
	return BirthdayGreeting{Celebrate: true, Name: name}, nil
}
