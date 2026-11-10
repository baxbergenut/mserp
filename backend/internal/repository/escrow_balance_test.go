package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEscrowBalanceAdjustmentDatabase(t *testing.T) {
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
		err = repo.SaveBalance(ctx, escrow, actor, input)
		if !errors.As(err, &dbErr) || dbErr.Code != "23514" {
			t.Fatalf("future collections must not fund the reduction: %v", err)
		}
		input.Balance = "125.25"
		check(repo.SaveBalance(ctx, escrow, actor, input))
		if err := repo.SaveBalance(ctx, escrow, actor, input); !errors.Is(err, ErrEscrowReleaseConflict) {
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
		_, err = pool.Exec(ctx, `DELETE FROM driver_escrow_payments WHERE escrow_id=$1 AND week_start=$2::date+7`, escrow, week)
		check(err)
		increase := EscrowBalanceInput{Balance: "2500", Version: get().Version, Reason: "Reconciled opening ledger"}
		check(repo.SaveBalance(ctx, escrow, actor, increase))
		account = get()
		if account.HeldAmount != "2500.00" || account.RemainingAmount != "0.00" ||
			account.PaidAmount != "250.00" || account.ReleasedAmount != "0" ||
			len(account.Payments) != 1 || len(account.Releases) != 2 || account.Releases[0].Amount != "-2474.75" {
			t.Fatalf("unexpected account after increase: %+v", account)
		}
		increase.Version = account.Version
		check(repo.SaveBalance(ctx, escrow, actor, increase))
		if after := get(); after.Version != account.Version || len(after.Releases) != 2 {
			t.Fatalf("unchanged balance produced a ledger movement: %+v", after)
		}
		check(pool.QueryRow(ctx, `SELECT escrow_collection_available($1,$2::date-7)::text`, escrow, week).Scan(&available))
		if available != "250.00" {
			t.Fatalf("earlier collection must leave capacity for the balance increase: %s", available)
		}
		_, err = pool.Exec(ctx, `UPDATE driver_escrow_payments SET amount=251 WHERE escrow_id=$1 AND week_start=$2::date-7`, escrow, week)
		if !errors.As(err, &dbErr) || dbErr.Code != "23514" {
			t.Fatalf("earlier payroll payment must respect the balance increase: %v", err)
		}
		tx, err = pool.Begin(ctx)
		check(err)
		defer tx.Rollback(ctx)
		rows, err := escrowDeductions(ctx, tx, driver, week)
		check(err)
		if len(rows) != 0 {
			t.Fatalf("increase to the full balance left a same-week deduction: %+v", rows)
		}
		weekStart, err := time.Parse(time.DateOnly, week)
		check(err)
		priorWeek := weekStart.AddDate(0, 0, -7).Format(time.DateOnly)
		batched, err := payrollBalanceBatchContext(ctx, tx, []payrollBalanceScope{{Week: priorWeek, Drivers: []string{driver}}})
		check(err)
		for _, read := range []context.Context{ctx, batched} {
			rows, err = escrowDeductions(read, tx, driver, priorWeek)
			check(err)
			if len(rows) != 1 || rows[0].Available != "250.00" {
				t.Fatalf("earlier payroll did not reserve capacity for the balance increase: %+v", rows)
			}
		}
		check(tx.Rollback(ctx))

		var orphan string
		check(pool.QueryRow(ctx, `INSERT INTO driver_escrows(driver_name,start_date,amount)
 VALUES('Unlinked historical escrow',$1,500) RETURNING id`, week).Scan(&orphan))
		check(repo.SaveBalance(ctx, orphan, actor, EscrowBalanceInput{Balance: "100", Version: 1, Reason: "Reconciled historical escrow"}))
	})
}
