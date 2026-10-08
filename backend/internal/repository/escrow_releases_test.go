package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func verifyEscrowReleases(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	week, _ := time.Parse(time.DateOnly, ChargeCurrentWeek())
	var driver, escrow string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Release driver','release driver','cpm',0.75,false) RETURNING id`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount) VALUES($1,'Release driver',$2,2500) RETURNING id`, driver, week).Scan(&escrow); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES($1,$2,2500)`, escrow, week); err != nil {
		t.Fatal(err)
	}
	repo := NewEscrowRepository(pool)
	pay := NewDriverPayRepository(pool)
	balance := func() Escrow {
		t.Helper()
		result, err := repo.List(ctx, EscrowQuery{DriverID: driver, IncludeInactive: true})
		if err != nil || len(result.Items) != 1 {
			t.Fatalf("balance %+v %v", result, err)
		}
		return result.Items[0]
	}
	newID := func() string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	input := EscrowReleaseInput{ID: newID(), WeekStart: week.Format(time.DateOnly), Amount: "100.25", EscrowVersion: balance().Version}
	past := input
	past.WeekStart = week.AddDate(0, 0, -7).Format(time.DateOnly)
	if err := repo.SaveRelease(ctx, escrow, past, ""); err == nil {
		t.Fatal("past release accepted")
	}
	excess := input
	excess.Amount = "2500.01"
	if err := repo.SaveRelease(ctx, escrow, excess, ""); err == nil {
		t.Fatal("over-release accepted")
	}
	if err := repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRelease(ctx, escrow, input, ""); !errors.Is(err, ErrEscrowReleaseConflict) {
		t.Fatalf("duplicate/stale release accepted: %v", err)
	}
	b := balance()
	if b.HeldAmount != "2399.75" || b.ReleasedAmount != "100.25" || b.PaidAmount != "2500.00" || b.RemainingAmount != "100.25" || b.Status != "partially_released" {
		t.Fatalf("release did not reopen the balance: %+v", b)
	}
	assertCredit := func(w time.Time, want string) {
		t.Helper()
		r, err := pay.GetDriverWeek(ctx, w, driver)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, d := range r.Drivers {
			for _, c := range d.AutoCharges {
				if c.Source == "escrow-release:"+input.ID {
					got = c.Amount
				}
			}
		}
		if got != want {
			t.Fatalf("credit %s got %s want %s", w, got, want)
		}
	}
	assertCredit(week, "100.25")            // Explicit releases must pay even an inactive driver.
	assertCredit(week.AddDate(0, 0, 7), "") // No carry or duplicate credit.
	input.Version = b.Releases[0].Version
	input.EscrowVersion = b.Version
	input.Amount = "200.50"
	input.WeekStart = week.AddDate(0, 0, 7).Format(time.DateOnly)
	if err := repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	assertCredit(week, "")
	assertCredit(week.AddDate(0, 0, 7), "200.50")
	// Existing collection edits cannot spend funds reserved by a release.
	if _, err := pool.Exec(ctx, `UPDATE driver_escrow_payments SET amount=100 WHERE escrow_id=$1`, escrow); err == nil {
		t.Fatal("collection reduced below release")
	}
	b = balance()
	input.Version = b.Releases[0].Version
	input.EscrowVersion = b.Version
	input.Cancelled = true
	if err := repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	if balance().HeldAmount != "2500.00" {
		t.Fatal("cancel did not restore funds")
	}
	assertCredit(week.AddDate(0, 0, 7), "")
	input = EscrowReleaseInput{ID: newID(), WeekStart: week.Format(time.DateOnly), Amount: "500.00", EscrowVersion: balance().Version}
	if err := repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	report, err := pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := pay.Settle(ctx, week, driver, report.Revision, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	b = balance()
	for _, r := range b.Releases {
		if r.ID == input.ID {
			input.Version = r.Version
			if r.Editable {
				t.Fatal("finalized release editable")
			}
		}
	}
	input.EscrowVersion = b.Version
	input.Amount = "600.00"
	if err = repo.SaveRelease(ctx, escrow, input, ""); err == nil {
		t.Fatal("finalized release edited")
	}
	assertCredit(week, "500.00")
	if _, err = pay.Settle(ctx, week, driver, finalized.Revision, "", "Correct release", true); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	assertCredit(week, "600.00")
	history, err := pay.History(ctx, driver, Pagination{PageSize: 100})
	if err != nil || history.Total == 0 {
		t.Fatalf("release missing from history: %+v %v", history, err)
	}
	var events int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_escrow_release_events WHERE release_id IN(SELECT id FROM driver_escrow_releases WHERE escrow_id=$1)`, escrow).Scan(&events); err != nil || events != 5 {
		t.Fatalf("audit events=%d err=%v", events, err)
	}
	// Two accountants cannot both spend the same displayed balance.
	sharedVersion := balance().Version
	results := make(chan error, 2)
	for _, id := range []string{newID(), newID()} {
		go func(id string) {
			results <- repo.SaveRelease(ctx, escrow, EscrowReleaseInput{ID: id, WeekStart: week.Format(time.DateOnly), Amount: "1000", EscrowVersion: sharedVersion}, "")
		}(id)
	}
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, ErrEscrowReleaseConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 || balance().HeldAmount != "900.00" {
		t.Fatal("concurrent releases reused the balance")
	}
	// A future saved collection cannot fund an earlier release.
	var future string
	if err = pool.QueryRow(ctx, `INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount) VALUES($1,'Release driver',$2,100) RETURNING id`, driver, week).Scan(&future); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES($1,$2,100)`, future, week.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = pool.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1`, future).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveRelease(ctx, future, EscrowReleaseInput{ID: newID(), WeekStart: week.Format(time.DateOnly), Amount: "100", EscrowVersion: version}, ""); err == nil {
		t.Fatal("release spent a future collection")
	}
	verifyEscrowReplenishment(t, ctx, pool)
}

func verifyEscrowReplenishment(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	week, _ := time.Parse(time.DateOnly, ChargeCurrentWeek())
	var driver, escrow, release string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Replenish driver','replenish driver','cpm',0.75,true) RETURNING id`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid) VALUES($1,'Replenish driver',$2,2500,2500) RETURNING id`, driver, week).Scan(&escrow); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&release); err != nil {
		t.Fatal(err)
	}
	repo, pay := NewEscrowRepository(pool), NewDriverPayRepository(pool)
	balance := func() Escrow {
		t.Helper()
		result, err := repo.List(ctx, EscrowQuery{DriverID: driver, IncludeInactive: true})
		if err != nil || len(result.Items) != 1 {
			t.Fatalf("balance: %+v %v", result, err)
		}
		return result.Items[0]
	}
	input := EscrowReleaseInput{ID: release, Amount: "500.25", WeekStart: week.Format(time.DateOnly), EscrowVersion: balance().Version}
	if err := repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	read := func(offset int) DriverPayEdits {
		t.Helper()
		r, err := pay.GetDriverWeek(ctx, week.AddDate(0, 0, offset*7), driver)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range r.Drivers {
			if d.ID == driver {
				return d.Edits
			}
		}
		return DriverPayEdits{}
	}
	if len(read(0).ExpenseDeductions) != 0 {
		t.Fatal("release collected in its own week")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES($1,$2,1)`, escrow, week); err == nil {
		t.Fatal("database allowed same-week replenishment")
	}
	collect := func(offset int, amount string) {
		t.Helper()
		edits := read(offset)
		if len(edits.ExpenseDeductions) != 1 {
			t.Fatalf("missing replenishment: %+v", edits)
		}
		edits.ExpenseDeductions[0].Amount = amount
		edits.ExpenseDeductions[0].Apply = true
		if _, err := pay.Save(ctx, edits); err != nil {
			t.Fatal(err)
		}
	}
	if d := read(1).ExpenseDeductions; len(d) != 1 || d[0].Amount != "500.25" {
		t.Fatalf("next week %+v", d)
	}
	collect(1, "100.10")
	if b := balance(); b.HeldAmount != "2099.85" || b.RemainingAmount != "400.15" || b.Status != "partial" {
		t.Fatalf("partial repayment %+v", b)
	}
	if d := read(2).ExpenseDeductions; len(d) != 1 || d[0].Amount != "400.15" {
		t.Fatalf("remaining repayment %+v", d)
	}
	collect(3, "150.05")
	if d := read(2).ExpenseDeductions; len(d) != 1 || d[0].Available != "250.10" {
		t.Fatalf("future repayment not reserved %+v", d)
	}
	// Release changes cannot leave already saved repayments overfunding escrow.
	b := balance()
	input.Version = b.Releases[0].Version
	input.EscrowVersion = b.Version
	input.Cancelled = true
	if err := repo.SaveRelease(ctx, escrow, input, ""); err == nil {
		t.Fatal("cancel accepted after repayment")
	}
	input.Cancelled = false
	input.WeekStart = week.AddDate(0, 0, 14).Format(time.DateOnly)
	if err := repo.SaveRelease(ctx, escrow, input, ""); err == nil {
		t.Fatal("release moved after repayment")
	}
	input.WeekStart = week.Format(time.DateOnly)
	input.Amount = "100"
	if err := repo.SaveRelease(ctx, escrow, input, ""); err == nil {
		t.Fatal("release reduced below repayments")
	}
	collect(2, "250.10")
	if b := balance(); b.HeldAmount != "2500.00" || b.RemainingAmount != "0.00" || b.PaidAmount != "3000.25" || b.Status != "paid" {
		t.Fatalf("replenished balance %+v", b)
	}
	if len(read(4).ExpenseDeductions) != 0 {
		t.Fatal("fully replenished escrow still collected")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES($1,$2,0.01)`, escrow, week.AddDate(0, 0, 28)); err == nil {
		t.Fatal("overcollection accepted")
	}
	// A scheduled release cannot reopen deductions before or during its own week.
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&release); err != nil {
		t.Fatal(err)
	}
	input = EscrowReleaseInput{ID: release, Amount: "200", WeekStart: week.AddDate(0, 0, 35).Format(time.DateOnly), EscrowVersion: balance().Version}
	if err := repo.SaveRelease(ctx, escrow, input, ""); err != nil {
		t.Fatal(err)
	}
	if len(read(4).ExpenseDeductions) != 0 || len(read(5).ExpenseDeductions) != 0 {
		t.Fatal("future release collected early")
	}
	if d := read(6).ExpenseDeductions; len(d) != 1 || d[0].Amount != "200.00" {
		t.Fatalf("future release not replenishable %+v", d)
	}
	if _, err := pool.Exec(ctx, `UPDATE drivers SET active=false WHERE id=$1`, driver); err != nil {
		t.Fatal(err)
	}
	if len(read(6).ExpenseDeductions) != 0 {
		t.Fatal("replenishment reintroduced an inactive driver")
	}
}
