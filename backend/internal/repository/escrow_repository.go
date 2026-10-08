package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EscrowRepository struct{ pool *pgxpool.Pool }

func NewEscrowRepository(pool *pgxpool.Pool) *EscrowRepository { return &EscrowRepository{pool: pool} }

type EscrowPayment struct {
	WeekStart string `json:"weekStart"`
	Amount    string `json:"amount"`
}

type Escrow struct {
	HeldAmount      string          `json:"heldAmount"`
	ReleasedAmount  string          `json:"releasedAmount"`
	Releases        []EscrowRelease `json:"releases"`
	ID              string          `json:"id"`
	DriverID        *string         `json:"driverId"`
	DriverName      string          `json:"driverName"`
	Active          bool            `json:"active"`
	StartDate       string          `json:"startDate"`
	Amount          string          `json:"amount"`
	OpeningPaid     string          `json:"openingPaid"`
	PaidAmount      string          `json:"paidAmount"`
	RemainingAmount string          `json:"remainingAmount"`
	Status          string          `json:"status"`
	Version         int             `json:"version"`
	Payments        []EscrowPayment `json:"payments"`
}
type EscrowQuery struct {
	Pagination
	Search, DriverID, Status, Group string
	IncludeInactive                 bool
}

type EscrowSummary struct {
	Held           string `json:"held"`
	Released       string `json:"released"`
	Target         string `json:"target"`
	Paid           string `json:"paid"`
	Remaining      string `json:"remaining"`
	Drivers        int    `json:"drivers"`
	PaidDrivers    int    `json:"paidDrivers"`
	PartialDrivers int    `json:"partialDrivers"`
	UnpaidDrivers  int    `json:"unpaidDrivers"`
}
type EscrowPage struct {
	Page[Escrow]
	Summary EscrowSummary `json:"summary"`
}

const escrowBalancesSQL = `SELECT e.id,e.driver_id,coalesce(d.full_name,e.driver_name) driver_name,
 coalesce(r.released,0) released_amount,e.opening_paid+coalesce(p.paid,0)-coalesce(r.released,0) held_amount,
 coalesce(d.active,false) active,e.start_date,e.amount,e.opening_paid,
 e.opening_paid+coalesce(p.paid,0) paid_amount,e.amount-e.opening_paid-coalesce(p.paid,0)+coalesce(r.released,0) remaining_amount,
 CASE WHEN NOT coalesce(d.active,false) THEN CASE WHEN coalesce(r.released,0)>0 AND e.opening_paid+coalesce(p.paid,0)-coalesce(r.released,0)=0 THEN 'released' WHEN e.opening_paid+coalesce(p.paid,0)-coalesce(r.released,0)>0 AND e.opening_paid+coalesce(p.paid,0)-coalesce(r.released,0)<e.amount THEN 'partially_released' ELSE 'not_released' END
 WHEN e.amount-e.opening_paid-coalesce(p.paid,0)+coalesce(r.released,0)<=0 THEN 'paid'
 WHEN e.opening_paid+coalesce(p.paid,0)-coalesce(r.released,0)>0 THEN 'partial' ELSE 'unpaid' END status,e.balance_version
 FROM driver_escrows e LEFT JOIN drivers d ON d.id=e.driver_id
 LEFT JOIN LATERAL (SELECT sum(amount) paid FROM driver_escrow_payments WHERE escrow_id=e.id) p ON true
 LEFT JOIN LATERAL (SELECT sum(amount) released FROM driver_escrow_releases WHERE escrow_id=e.id AND NOT cancelled) r ON true`

func (r *EscrowRepository) List(ctx context.Context, q EscrowQuery) (EscrowPage, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return EscrowPage{}, err
	}
	defer tx.Rollback(ctx)
	const filter = ` WHERE ($1='' OR driver_name ILIKE '%'||$1||'%') AND ($2='' OR driver_id::text=$2) AND ($3='' OR status=$3) AND ($4 OR active) AND ($5='' OR ($5='active' AND active) OR ($5='terminated' AND NOT active))`
	var total int
	var summary EscrowSummary
	if err = tx.QueryRow(ctx, `WITH balances AS (`+escrowBalancesSQL+`), filtered AS (SELECT * FROM balances`+filter+`),
 drivers AS (SELECT coalesce(driver_id,id) id,sum(held_amount) paid,sum(remaining_amount) remaining,sum(released_amount) released,bool_and(active) active FROM filtered WHERE active OR $5='terminated' GROUP BY coalesce(driver_id,id))
 SELECT count(*),coalesce(sum(held_amount),0)::text,coalesce(sum(released_amount),0)::text,coalesce(sum(amount),0)::text,coalesce(sum(paid_amount),0)::text,coalesce(sum(remaining_amount),0)::text,
 (SELECT count(*) FROM drivers),(SELECT count(*) FROM drivers WHERE (active AND remaining<=0) OR (NOT active AND released>0 AND paid=0)),
 (SELECT count(*) FROM drivers WHERE (active AND remaining>0 AND paid>0) OR (NOT active AND remaining>0 AND paid>0)),(SELECT count(*) FROM drivers WHERE (active AND remaining>0 AND paid=0) OR (NOT active AND (remaining<=0 OR (paid=0 AND released=0))))
 FROM filtered`, q.Search, q.DriverID, q.Status, q.IncludeInactive || q.Group == "terminated", q.Group).Scan(&total, &summary.Held, &summary.Released, &summary.Target, &summary.Paid, &summary.Remaining, &summary.Drivers, &summary.PaidDrivers, &summary.PartialDrivers, &summary.UnpaidDrivers); err != nil {
		return EscrowPage{}, err
	}
	page := q.Pagination.Normalize(total)
	rows, err := tx.Query(ctx, `WITH balances AS (`+escrowBalancesSQL+`) SELECT id,driver_id,driver_name,active,start_date::text,amount::text,opening_paid::text,paid_amount::text,remaining_amount::text,status,balance_version,held_amount::text,released_amount::text FROM balances`+filter+` ORDER BY lower(driver_name),start_date,id LIMIT $6 OFFSET $7`, q.Search, q.DriverID, q.Status, q.IncludeInactive || q.Group == "terminated", q.Group, page.PageSize, page.Offset())
	if err != nil {
		return EscrowPage{}, err
	}
	items := []Escrow{}
	for rows.Next() {
		var e Escrow
		if err = rows.Scan(&e.ID, &e.DriverID, &e.DriverName, &e.Active, &e.StartDate, &e.Amount, &e.OpeningPaid, &e.PaidAmount, &e.RemainingAmount, &e.Status, &e.Version, &e.HeldAmount, &e.ReleasedAmount); err != nil {
			rows.Close()
			return EscrowPage{}, err
		}
		e.Payments = []EscrowPayment{}
		e.Releases = []EscrowRelease{}
		items = append(items, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EscrowPage{}, err
	}
	for i := range items {
		payments, e := tx.Query(ctx, `SELECT week_start::text,amount::text FROM driver_escrow_payments WHERE escrow_id=$1 ORDER BY week_start DESC`, items[i].ID)
		if e != nil {
			return EscrowPage{}, e
		}
		for payments.Next() {
			var p EscrowPayment
			if e = payments.Scan(&p.WeekStart, &p.Amount); e != nil {
				payments.Close()
				return EscrowPage{}, e
			}
			items[i].Payments = append(items[i].Payments, p)
		}
		e = payments.Err()
		payments.Close()
		if e != nil {
			return EscrowPage{}, e
		}
		releases, releaseErr := tx.Query(ctx, `SELECT r.id::text,r.week_start::text,r.amount::text,r.cancelled,r.version,
 NOT r.cancelled AND $3::uuid IS NOT NULL AND r.week_start >= $2::date AND NOT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id=$3 AND s.week_start=r.week_start AND s.finalized)
 FROM driver_escrow_releases r WHERE r.escrow_id=$1 ORDER BY r.week_start DESC,r.created_at DESC`, items[i].ID, ChargeCurrentWeek(), items[i].DriverID)
		if releaseErr != nil {
			return EscrowPage{}, releaseErr
		}
		for releases.Next() {
			var release EscrowRelease
			if releaseErr = releases.Scan(&release.ID, &release.WeekStart, &release.Amount, &release.Cancelled, &release.Version, &release.Editable); releaseErr != nil {
				releases.Close()
				return EscrowPage{}, releaseErr
			}
			items[i].Releases = append(items[i].Releases, release)
		}
		releaseErr = releases.Err()
		releases.Close()
		if releaseErr != nil {
			return EscrowPage{}, releaseErr
		}
	}
	return EscrowPage{Page: NewPage(items, total, page), Summary: summary}, tx.Commit(ctx)
}

var ErrDriverEscrowSettingConflict = errors.New("driver escrow default changed; reload and try again")

type DriverEscrowSetting struct {
	DefaultAmount string `json:"defaultAmount"`
	Version       int    `json:"version"`
}

func (r *EscrowRepository) GetDriverEscrowSetting(ctx context.Context) (DriverEscrowSetting, error) {
	var value DriverEscrowSetting
	err := r.pool.QueryRow(ctx, `SELECT default_amount::text,version FROM driver_escrow_settings WHERE singleton`).Scan(&value.DefaultAmount, &value.Version)
	return value, err
}
func (r *EscrowRepository) SaveDriverEscrowSetting(ctx context.Context, input DriverEscrowSetting) (DriverEscrowSetting, error) {
	var value DriverEscrowSetting
	err := r.pool.QueryRow(ctx, `UPDATE driver_escrow_settings SET default_amount=$1::numeric,version=version+1,updated_at=now() WHERE singleton AND version=$2 RETURNING default_amount::text,version`, input.DefaultAmount, input.Version).Scan(&value.DefaultAmount, &value.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrDriverEscrowSettingConflict
	}
	return value, err
}

func escrowDeductions(ctx context.Context, tx pgx.Tx, driver, week string) ([]ExpenseDeduction, error) {
	all, err := escrowDeductionsBulk(ctx, tx, []string{driver}, week)
	values := all[driver]
	if values == nil {
		values = []ExpenseDeduction{}
	}
	return values, err
}
