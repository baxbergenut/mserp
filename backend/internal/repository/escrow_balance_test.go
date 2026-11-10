package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEscrowBalanceReductionDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		var driver, escrow, actor string
		check(pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate)
 VALUES('Balance Driver','balance driver','cpm',0.75) RETURNING id`).Scan(&driver))
		check(pool.QueryRow(ctx, `INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount)
 VALUES($1,'Balance Driver',$2::date-7,2500) RETURNING id`, driver, ChargeCurrentWeek()).Scan(&escrow))
		check(pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash)
 VALUES('escrow correction test',crypt('test-only',gen_salt('bf'))) RETURNING id`).Scan(&actor))
		week := ChargeCurrentWeek()
		_, err := pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount)
 VALUES($1,$2::date-7,250),($1,$2::date+7,100)`, escrow, week)
		check(err)
		repo := NewEscrowRepository(pool)
		get := func() Escrow {
			t.Helper()
			page, err := repo.List(ctx, EscrowQuery{DriverID: driver})
			check(err)
			if len(page.Items) != 1 {
				t.Fatalf("expected one account, got %+v", page)
			}
			return page.Items[0]
		}
		input := EscrowBalanceInput{Balance: "0", Version: get().Version, Reason: "Verified ledger correction"}
		var dbErr *pgconn.PgError
		err = repo.ReduceBalance(ctx, escrow, actor, input)
		if !errors.As(err, &dbErr) || dbErr.Code != "23514" {
			t.Fatalf("future collections must not fund the reduction: %v", err)
		}
		input.Balance = "125.25"
		check(repo.ReduceBalance(ctx, escrow, actor, input))
		if err := repo.ReduceBalance(ctx, escrow, actor, input); !errors.Is(err, ErrEscrowReleaseConflict) {
			t.Fatalf("stale retry: %v", err)
		}
		account := get()
		if account.HeldAmount != "125.25" || account.RemainingAmount != "2374.75" ||
			account.OpeningPaid != "0.00" || account.PaidAmount != "350.00" || account.ReleasedAmount != "0" ||
			len(account.Payments) != 2 || len(account.Releases) != 1 {
			t.Fatalf("unexpected account after correction: %+v", account)
		}
		correction := account.Releases[0]
		if correction.Editable || correction.WeekStart != week || correction.Amount != "224.75" ||
			correction.CorrectionReason == nil || *correction.CorrectionReason != input.Reason {
			t.Fatalf("unexpected correction: %+v", correction)
		}
		var auditCount int
		check(pool.QueryRow(ctx, `SELECT count(*) FROM driver_escrow_release_events
 WHERE release_id=$1 AND actor_id=$2 AND after_value->>'correction_reason'=$3`,
			correction.ID, actor, input.Reason).Scan(&auditCount))
		if auditCount != 1 {
			t.Fatalf("expected one correction audit, got %d", auditCount)
		}
		tx, err := pool.Begin(ctx)
		check(err)
		defer tx.Rollback(ctx)
		credits, err := escrowReleaseCredits(ctx, tx, driver, week)
		check(err)
		if len(credits) != 0 {
			t.Fatalf("balance correction produced a payroll credit: %+v", credits)
		}
		var available string
		check(tx.QueryRow(ctx, `SELECT escrow_collection_available($1,$2::date+14)::text`, escrow, week).Scan(&available))
		if available != "2374.75" {
			t.Fatalf("future collection capacity: %s", available)
		}
		check(tx.Rollback(ctx))
		_, err = pool.Exec(ctx, `UPDATE driver_escrow_releases SET amount=1 WHERE id=$1`, correction.ID)
		if !errors.As(err, &dbErr) || dbErr.Code != "23514" {
			t.Fatalf("expected immutable correction: %v", err)
		}
	})
}
