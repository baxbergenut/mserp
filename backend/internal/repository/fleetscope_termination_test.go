package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
)

func testFleetScopeOffboarding(t *testing.T, pool, admin *pgxpool.Pool) {
	ctx := context.Background()
	repo := NewFleetRepository(pool)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	uuid := func() string {
		var id string
		must(pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id))
		return id
	}
	dispatcher, err := repo.CreateDispatcher(ctx, DispatcherInput{FullName: "Offboarding Dispatch", Active: true})
	must(err)
	truck, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "OFFBOARD", Status: "available", Active: true})
	must(err)
	input := DriverInput{FullName: "Offboarding Driver", PayType: "cpm", PayRate: .75, Active: true, TruckID: &truck.ID, DispatcherID: &dispatcher.ID}
	driver, err := repo.CreateDriver(ctx, input)
	must(err)
	// Old clients can submit their existing assignment IDs while switching active off.
	input.Active = false
	driver, err = repo.UpdateDriver(ctx, driver.ID, input)
	must(err)
	if driver.Active || driver.TruckID != nil || driver.DispatcherID != nil {
		t.Fatalf("driver links retained: %+v", driver)
	}
	truck, err = repo.GetTruck(ctx, truck.ID)
	must(err)
	if truck.DriverID != nil || truck.Status != "available" {
		t.Fatal("truck not released")
	}
	input.Active = true
	driver, err = repo.UpdateDriver(ctx, driver.ID, input)
	must(err)
	truck, err = repo.UpdateTruck(ctx, truck.ID, TruckInput{UnitNumber: truck.UnitNumber, Status: "assigned", Active: false, DriverID: &driver.ID})
	must(err)
	if truck.DriverID != nil || truck.Active || truck.Status == "assigned" {
		t.Fatal("inactive truck kept assignment")
	}
	driver, err = repo.GetDriver(ctx, driver.ID)
	must(err)
	if !driver.Active || driver.TruckID != nil || driver.DispatcherID == nil {
		t.Fatal("truck deactivation changed unrelated driver state")
	}
	// Imports cannot resurrect assignments, but retain historical fleet identity.
	tx, err := pool.Begin(ctx)
	must(err)
	must(syncTruckAssignment(ctx, tx, truck.ID, driver.ID))
	must(tx.Commit(ctx))
	truck, err = repo.GetTruck(ctx, truck.ID)
	must(err)
	if truck.DriverID != nil {
		t.Fatal("import assigned inactive truck")
	}
	truck, err = repo.UpdateTruck(ctx, truck.ID, TruckInput{UnitNumber: truck.UnitNumber, Status: "available", Active: true, DriverID: &driver.ID})
	must(err)
	company := uuid()
	hire := fleetscope.Event{Version: 1, Type: "driver.hired", EventID: uuid(), CompanyID: company, OccurredAt: time.Now(), Driver: fleetscope.Driver{ID: uuid(), FullName: driver.FullName, DriverType: "company", HireDate: "2026-09-28"}}
	intake, err := repo.AcceptFleetScopeHire(ctx, hire, "hire-offboard")
	must(err)
	_, err = repo.CompleteDriverIntake(ctx, intake.IntakeID, "00000000-0000-0000-0000-000000000001", driver.ID, nil, false)
	must(err)
	term := fleetscope.Event{Version: 1, Type: "driver.terminated", EventID: uuid(), CompanyID: company, OccurredAt: time.Now(), TerminationDate: "2026-10-05", Driver: fleetscope.Driver{ID: hire.Driver.ID, FullName: driver.FullName}}
	var wg sync.WaitGroup
	results := make(chan IntakeResult, 6)
	failures := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.AcceptFleetScopeTermination(ctx, term, "term-original")
			if err != nil {
				failures <- err
			} else {
				results <- result
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		must(err)
	}
	accepted := 0
	terminationID := ""
	for result := range results {
		if result.Status == "accepted" {
			accepted++
		}
		terminationID = result.TerminationID
	}
	if accepted != 1 || terminationID == "" {
		t.Fatal("termination not deduplicated")
	}
	driver, err = repo.GetDriver(ctx, driver.ID)
	must(err)
	if driver.Active || driver.TruckID != nil || driver.DispatcherID != nil || driver.PayRate != .75 {
		t.Fatal("termination did not safely deactivate")
	}
	truck, err = repo.GetTruck(ctx, truck.ID)
	must(err)
	if truck.DriverID != nil || truck.Status != "available" || truck.OwnerID == "" {
		t.Fatal("termination damaged truck")
	}
	var n int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM truck_driver_assignments WHERE driver_id=$1 AND unassigned_at IS NOT NULL`, driver.ID).Scan(&n))
	if n < 3 {
		t.Fatal("assignment history lost")
	}
	must(pool.QueryRow(ctx, `SELECT count(*) FROM driver_dispatcher_assignments WHERE driver_id=$1 AND dispatcher_id IS NOT NULL AND unassigned_at IS NULL`, driver.ID).Scan(&n))
	if n != 0 {
		t.Fatal("dispatcher history not closed")
	}
	var notes string
	must(pool.QueryRow(ctx, `SELECT notes FROM custom_tasks WHERE id=(SELECT task_id FROM fleetscope_driver_terminations WHERE id=$1)`, terminationID).Scan(&notes))
	if !strings.Contains(notes, "current truck and dispatcher assignments disconnected") {
		t.Fatal(notes)
	}
	taskCount := func() int {
		var count int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM custom_tasks`).Scan(&count))
		return count
	}
	before := taskCount()
	_, err = repo.AcceptFleetScopeTermination(ctx, term, "different-body")
	if !errors.Is(err, ErrFleetScopeEventConflict) {
		t.Fatal("event collision accepted", err)
	}
	term.EventID = uuid()
	duplicate, err := repo.AcceptFleetScopeTermination(ctx, term, "different-event")
	must(err)
	if duplicate.Status != "duplicate" || taskCount() != before {
		t.Fatal("new event ID repeated offboarding")
	}
	tx, err = pool.Begin(ctx)
	must(err)
	matched, found, err := resolveDriver(ctx, tx, driver.FullName, &dispatcher.ID, false)
	must(err)
	if !found || matched != driver.ID {
		t.Fatal("historical identity lost")
	}
	must(syncTruckAssignment(ctx, tx, truck.ID, driver.ID))
	must(tx.Commit(ctx))
	driver, err = repo.GetDriver(ctx, driver.ID)
	must(err)
	if driver.Active || driver.TruckID != nil || driver.DispatcherID != nil {
		t.Fatal("import reactivated driver")
	}
	if _, err = repo.CreateTruck(ctx, TruckInput{UnitNumber: "REJECT-INACTIVE", Status: "available", Active: true, DriverID: &driver.ID}); err == nil {
		t.Fatal("manual assignment to inactive driver accepted")
	}
	// A deleted task or local driver never causes redelivery to recreate work.
	_, err = pool.Exec(ctx, `DELETE FROM custom_tasks WHERE id=(SELECT task_id FROM fleetscope_driver_terminations WHERE id=$1)`, terminationID)
	must(err)
	must(repo.DeleteDriver(ctx, driver.ID))
	before = taskCount()
	duplicate, err = repo.AcceptFleetScopeTermination(ctx, term, "different-event")
	must(err)
	if duplicate.Status != "duplicate" || taskCount() != before {
		t.Fatal("retry recreated deleted task")
	}

	for _, order := range []string{"pending", "termination-first", "unknown"} {
		t.Run(order, func(t *testing.T) {
			h := hire
			h.Driver.ID = uuid()
			h.EventID = uuid()
			h.Driver.FullName = "Review " + order
			var i IntakeResult
			if order == "pending" {
				i, err = repo.AcceptFleetScopeHire(ctx, h, "hire-"+order)
				must(err)
			}
			e := term
			e.EventID = uuid()
			e.Driver = fleetscope.Driver{ID: h.Driver.ID, FullName: h.Driver.FullName}
			result, err := repo.AcceptFleetScopeTermination(ctx, e, "term-"+order)
			must(err)
			if order == "termination-first" {
				i, err = repo.AcceptFleetScopeHire(ctx, h, "hire-"+order)
				must(err)
			}
			if i.IntakeID != "" {
				if _, err = repo.GetDriverIntake(ctx, i.IntakeID); !errors.Is(err, ErrNotFound) {
					t.Fatal("terminated hire still visible", err)
				}
				if _, err = repo.CompleteDriverIntake(ctx, i.IntakeID, "00000000-0000-0000-0000-000000000001", "", &input, true); !errors.Is(err, ErrIntakeTerminated) {
					t.Fatal("terminated hire completed", err)
				}
			}
			page, err := repo.ListDriverDirectory(ctx, Pagination{}, h.Driver.FullName, false)
			must(err)
			if page.Total != 0 {
				t.Fatal("terminated pending driver in directory")
			}
			must(pool.QueryRow(ctx, `SELECT notes FROM custom_tasks WHERE id=(SELECT task_id FROM fleetscope_driver_terminations WHERE id=$1)`, result.TerminationID).Scan(&notes))
			if !strings.Contains(notes, "Review") && !strings.Contains(notes, "ACTION REQUIRED") {
				t.Fatal("missing review task")
			}
		})
	}
	// A task persistence failure rolls back deactivation, assignment release and receipt.
	rollbackInput := DriverInput{FullName: "Rollback Driver", Active: true, PayType: "cpm", PayRate: .75, TruckID: &truck.ID, DispatcherID: &dispatcher.ID}
	rollbackDriver, err := repo.CreateDriver(ctx, rollbackInput)
	must(err)
	h := hire
	h.EventID, h.Driver.ID, h.Driver.FullName = uuid(), uuid(), rollbackInput.FullName
	i, err := repo.AcceptFleetScopeHire(ctx, h, "rollback-hire")
	must(err)
	_, err = repo.CompleteDriverIntake(ctx, i.IntakeID, "00000000-0000-0000-0000-000000000001", rollbackDriver.ID, nil, false)
	must(err)
	_, err = admin.Exec(ctx, `ALTER TABLE custom_tasks ADD CONSTRAINT offboard_test_reject CHECK(title <> 'Offboard Rollback Driver')`)
	must(err)
	e := term
	e.EventID, e.Driver = uuid(), fleetscope.Driver{ID: h.Driver.ID, FullName: h.Driver.FullName}
	if _, err = repo.AcceptFleetScopeTermination(ctx, e, "rollback-term"); err == nil {
		t.Fatal("expected task failure")
	}
	rollbackDriver, err = repo.GetDriver(ctx, rollbackDriver.ID)
	must(err)
	if !rollbackDriver.Active || rollbackDriver.TruckID == nil || rollbackDriver.DispatcherID == nil {
		t.Fatal("failed receipt changed driver")
	}
	must(pool.QueryRow(ctx, `SELECT count(*) FROM fleetscope_webhook_receipts WHERE event_id=$1`, e.EventID).Scan(&n))
	if n != 0 {
		t.Fatal("failed receipt persisted")
	}
	_, err = admin.Exec(ctx, `ALTER TABLE custom_tasks DROP CONSTRAINT offboard_test_reject`)
	must(err)
	_, err = repo.AcceptFleetScopeTermination(ctx, e, "rollback-term")
	must(err)

	// Matching a display name alone never authorizes changing an existing driver.
	unlinked, err := repo.CreateDriver(ctx, DriverInput{FullName: "Unlinked Driver", Active: true, PayType: "cpm", PayRate: .75})
	must(err)
	e.EventID, e.Driver = uuid(), fleetscope.Driver{ID: uuid(), FullName: unlinked.FullName}
	_, err = repo.AcceptFleetScopeTermination(ctx, e, "unlinked-term")
	must(err)
	unlinked, err = repo.GetDriver(ctx, unlinked.ID)
	must(err)
	if !unlinked.Active {
		t.Fatal("unlinked driver was matched by name")
	}
	// Pausing charge conflicts is all-or-nothing, while operational offboarding succeeds.
	for _, conflict := range []bool{false, true} {
		name := fmt.Sprintf("Charge Offboard %v", conflict)
		d, err := repo.CreateDriver(ctx, DriverInput{FullName: name, Active: true, PayType: "cpm", PayRate: .5})
		must(err)
		h := hire
		h.EventID = uuid()
		h.Driver.ID = uuid()
		h.Driver.FullName = name
		i, err := repo.AcceptFleetScopeHire(ctx, h, "charge-hire-"+name)
		must(err)
		_, err = repo.CompleteDriverIntake(ctx, i.IntakeID, "00000000-0000-0000-0000-000000000001", d.ID, nil, false)
		must(err)
		var schedule string
		must(pool.QueryRow(ctx, `INSERT INTO driver_charge_schedules(driver_id,kind,name,direction,start_week,eligibility,total) VALUES($1,'installment','Equipment','charge',$2::date,'calendar',200) RETURNING id`, d.ID, ChargeCurrentWeek()).Scan(&schedule))
		_, err = pool.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES($1,$2::date,50)`, schedule, ChargeCurrentWeek())
		must(err)
		if conflict {
			_, err = pool.Exec(ctx, `INSERT INTO driver_charge_occurrences(schedule_id,week_start,name,scheduled_amount,amount,overridden) VALUES($1,$2::date,'Equipment',50,25,true)`, schedule, ChargeCurrentWeek())
			must(err)
		}
		e := term
		e.EventID = uuid()
		e.Driver = fleetscope.Driver{ID: h.Driver.ID, FullName: name}
		result, err := repo.AcceptFleetScopeTermination(ctx, e, "charge-term-"+name)
		must(err)
		d, err = repo.GetDriver(ctx, d.ID)
		must(err)
		if d.Active {
			t.Fatal("charge handling prevented deactivation")
		}
		var paused bool
		must(pool.QueryRow(ctx, `SELECT paused FROM driver_charge_phases WHERE schedule_id=$1`, schedule).Scan(&paused))
		if paused == conflict {
			t.Fatal("incorrect charge pause behavior")
		}
		must(pool.QueryRow(ctx, `SELECT notes FROM custom_tasks WHERE id=(SELECT task_id FROM fleetscope_driver_terminations WHERE id=$1)`, result.TerminationID).Scan(&notes))
		if conflict && !strings.Contains(notes, "Charges were not paused") {
			t.Fatal("charge exception not actionable")
		}
	}
}
