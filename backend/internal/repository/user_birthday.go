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
	var name string
	err := r.pool.QueryRow(ctx, `UPDATE app_users SET birthday_greeted_year=$2
 WHERE id=$1 AND active AND extract(month FROM date_of_birth)=$3
 AND extract(day FROM date_of_birth)=$4 AND birthday_greeted_year IS DISTINCT FROM $2
 RETURNING coalesce(nullif(first_name,''),username)`, userID, day.Year(), int(day.Month()), day.Day()).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		// Prepare the next birthday midnight without claiming its greeting early.
		tomorrow := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, day.Location())
		var eligible bool
		err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_users WHERE id=$1 AND active
		 AND extract(month FROM date_of_birth)=$2 AND extract(day FROM date_of_birth)=$3
		 AND birthday_greeted_year IS DISTINCT FROM $4)`, userID, int(tomorrow.Month()), tomorrow.Day(), tomorrow.Year()).Scan(&eligible)
		if err != nil {
			return BirthdayGreeting{}, err
		}
		if eligible {
			return BirthdayGreeting{CountdownAt: &tomorrow}, nil
		}
		return BirthdayGreeting{}, nil
	}
	if err != nil {
		return BirthdayGreeting{}, err
	}
	return BirthdayGreeting{Celebrate: true, Name: name}, nil
}
