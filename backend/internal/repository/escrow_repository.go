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
	Search, DriverID, Status string
	IncludeInactive          bool
}

type EscrowSummary struct {
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
 coalesce(d.active,false) active,e.start_date,e.amount,e.opening_paid,
 e.opening_paid+coalesce(p.paid,0) paid_amount,e.amount-e.opening_paid-coalesce(p.paid,0) remaining_amount,
 CASE WHEN e.amount-e.opening_paid-coalesce(p.paid,0)<=0 THEN 'paid'
 WHEN e.opening_paid+coalesce(p.paid,0)>0 THEN 'partial' ELSE 'unpaid' END status,e.balance_version
 FROM driver_escrows e LEFT JOIN drivers d ON d.id=e.driver_id
 LEFT JOIN LATERAL (SELECT sum(amount) paid FROM driver_escrow_payments WHERE escrow_id=e.id) p ON true`

func (r *EscrowRepository) List(ctx context.Context, q EscrowQuery) (EscrowPage, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return EscrowPage{}, err
	}
	defer tx.Rollback(ctx)
	const filter = ` WHERE ($1='' OR driver_name ILIKE '%'||$1||'%') AND ($2='' OR driver_id::text=$2) AND ($3='' OR status=$3) AND ($4 OR active)`
	var total int
	var summary EscrowSummary
	if err = tx.QueryRow(ctx, `WITH balances AS (`+escrowBalancesSQL+`), filtered AS (SELECT * FROM balances`+filter+`),
 drivers AS (SELECT coalesce(driver_id,id) id,sum(paid_amount) paid,sum(remaining_amount) remaining FROM filtered WHERE active GROUP BY coalesce(driver_id,id))
 SELECT count(*),coalesce(sum(amount),0)::text,coalesce(sum(paid_amount),0)::text,coalesce(sum(remaining_amount),0)::text,
 (SELECT count(*) FROM drivers),(SELECT count(*) FROM drivers WHERE remaining<=0),
 (SELECT count(*) FROM drivers WHERE remaining>0 AND paid>0),(SELECT count(*) FROM drivers WHERE remaining>0 AND paid=0)
 FROM filtered`, q.Search, q.DriverID, q.Status, q.IncludeInactive).Scan(&total, &summary.Target, &summary.Paid, &summary.Remaining, &summary.Drivers, &summary.PaidDrivers, &summary.PartialDrivers, &summary.UnpaidDrivers); err != nil {
		return EscrowPage{}, err
	}
	page := q.Pagination.Normalize(total)
	rows, err := tx.Query(ctx, `WITH balances AS (`+escrowBalancesSQL+`) SELECT id,driver_id,driver_name,active,start_date::text,amount::text,opening_paid::text,paid_amount::text,remaining_amount::text,status,balance_version FROM balances`+filter+` ORDER BY lower(driver_name),start_date,id LIMIT $5 OFFSET $6`, q.Search, q.DriverID, q.Status, q.IncludeInactive, page.PageSize, page.Offset())
	if err != nil {
		return EscrowPage{}, err
	}
	items := []Escrow{}
	for rows.Next() {
		var e Escrow
		if err = rows.Scan(&e.ID, &e.DriverID, &e.DriverName, &e.Active, &e.StartDate, &e.Amount, &e.OpeningPaid, &e.PaidAmount, &e.RemainingAmount, &e.Status, &e.Version); err != nil {
			rows.Close()
			return EscrowPage{}, err
		}
		e.Payments = []EscrowPayment{}
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
	rows, err := tx.Query(ctx, `SELECT e.id,e.start_date::text,e.amount::text,
 (e.amount-e.opening_paid-coalesce(p.other,0))::text,
 coalesce(w.amount,e.amount-e.opening_paid-coalesce(p.other,0))::text,
 (e.amount-e.opening_paid-coalesce(p.prior,0)-coalesce(w.amount,0))::text,
 (e.amount-e.opening_paid-coalesce(p.prior,0))::text,e.balance_version,w.escrow_id IS NOT NULL
 FROM driver_escrows e
 LEFT JOIN driver_escrow_payments w ON w.escrow_id=e.id AND w.week_start=$2::date
 LEFT JOIN LATERAL (SELECT sum(amount) FILTER(WHERE week_start<>$2::date) other,sum(amount) FILTER(WHERE week_start<$2::date) prior FROM driver_escrow_payments WHERE escrow_id=e.id) p ON true
 WHERE e.driver_id=$1 AND e.start_date<$2::date+7
 AND (w.escrow_id IS NOT NULL OR e.amount>e.opening_paid+coalesce(p.other,0)) ORDER BY e.start_date,e.id`, driver, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExpenseDeduction{}
	for rows.Next() {
		d := ExpenseDeduction{Name: "Escrow", Source: "escrow"}
		if err = rows.Scan(&d.ExpenseID, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
