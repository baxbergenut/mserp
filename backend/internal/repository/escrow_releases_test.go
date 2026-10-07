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
	if b.HeldAmount != "2399.75" || b.ReleasedAmount != "100.25" || b.PaidAmount != "2500.00" || b.RemainingAmount != "0.00" || b.Status != "paid" {
		t.Fatalf("release changed collection principal: %+v", b)
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
}
