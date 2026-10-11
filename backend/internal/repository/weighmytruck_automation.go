package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type WMTRequest struct {
	DriverID string `json:"driverId"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Error    string `json:"error"`
}

func queueWMTEnrollment(ctx context.Context, tx pgx.Tx, driver, actor string) error {
	var name, email, phone, status string
	if e := tx.QueryRow(ctx, `SELECT full_name,coalesce(email,''),coalesce(phone,''),status FROM drivers WHERE id=$1`, driver).Scan(&name, &email, &phone, &status); e != nil {
		return e
	}
	if status == "terminated" {
		return &WMTValidationError{"Terminated drivers cannot be added to WeighMyTruck."}
	}
	if _, e := wmtDriver(name, email, phone); e != nil {
		return e
	}
	var initialized bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_imports)`).Scan(&initialized); e != nil {
		return e
	}
	if !initialized {
		return &WMTValidationError{"Initialize the WeighMyTruck roster before requesting enrollment."}
	}
	_, e := tx.Exec(ctx, `INSERT INTO weighmytruck_requests(driver_id,actor_id) VALUES($1,$2) ON CONFLICT(driver_id) DO NOTHING`, driver, actor)
	return e
}

// Queue intent is committed with onboarding. A crashed or uncertain provider
// mutation is never blindly replayed; its durable membership state needs review.
func (r *WeighMyTruckRepository) ProcessAutomation(ctx context.Context) error {
	if r.provider == nil || !r.provider.Configured() {
		return nil
	}
	rows, e := r.pool.Query(ctx, `SELECT driver_id::text,actor_id FROM weighmytruck_requests WHERE state='queued' ORDER BY created_at LIMIT 20`)
	if e != nil {
		return e
	}
	type request struct{ driver, actor string }
	requests := []request{}
	for rows.Next() {
		var q request
		if e = rows.Scan(&q.driver, &q.actor); e != nil {
			rows.Close()
			return e
		}
		requests = append(requests, q)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, q := range requests {
		tag, e := r.pool.Exec(ctx, `UPDATE weighmytruck_requests SET state='running',updated_at=now() WHERE driver_id=$1 AND state='queued'`, q.driver)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		var in WMTChange
		in.DriverID = q.driver
		in.Add = true
		var enrolled bool
		var state, status string
		e = r.pool.QueryRow(ctx, `SELECT coalesce(m.id::text,''),coalesce(m.version,0),coalesce(m.enrolled,false),coalesce(m.state,'confirmed'),d.status FROM drivers d LEFT JOIN weighmytruck_memberships m ON m.driver_id=d.id WHERE d.id=$1`, q.driver).Scan(&in.MembershipID, &in.Version, &enrolled, &state, &status)
		if e == nil && status != "terminated" && (!enrolled || state != "confirmed") {
			e = r.Change(ctx, in, q.actor)
		}
		message, done := "", "done"
		if e != nil {
			message = "Enrollment needs review on the WeighMyTruck page."
			done = "review"
		}
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_, saveErr := r.pool.Exec(finish, `UPDATE weighmytruck_requests SET state=$2,last_error=$3,updated_at=now() WHERE driver_id=$1`, q.driver, done, message)
		cancel()
		if saveErr != nil {
			return saveErr
		}
	}
	// All termination paths (FleetScope, profile, status dialog) converge here.
	rows, e = r.pool.Query(ctx, `SELECT m.id::text,m.driver_id::text,m.version FROM weighmytruck_memberships m JOIN drivers d ON d.id=m.driver_id WHERE d.status='terminated' AND m.enrolled AND m.state='confirmed' AND (m.last_error='' OR m.updated_at<now()-interval '15 minutes') LIMIT 20`)
	if e != nil {
		return e
	}
	changes := []WMTChange{}
	for rows.Next() {
		var in WMTChange
		if e = rows.Scan(&in.MembershipID, &in.DriverID, &in.Version); e != nil {
			rows.Close()
			return e
		}
		changes = append(changes, in)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, in := range changes {
		if e = r.Change(ctx, in, "system:termination"); e != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	// Surface stale queue reservations; they must not look like a successful add.
	_, e = r.pool.Exec(ctx, `UPDATE weighmytruck_requests SET state='review',last_error='Enrollment was interrupted; review membership before retrying.',updated_at=now() WHERE state='running' AND updated_at<now()-interval '2 minutes'`)
	return e
}

func (r *WeighMyTruckRepository) RetryEnrollment(ctx context.Context, driver string) error {
	// A manual Add on the main page uses the normal membership/version guards.
	_, e := r.pool.Exec(ctx, `UPDATE weighmytruck_requests SET state='done',last_error='',updated_at=now() WHERE driver_id=$1 AND EXISTS(SELECT 1 FROM weighmytruck_memberships WHERE driver_id=$1 AND enrolled AND state='confirmed')`, driver)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	return e
}
