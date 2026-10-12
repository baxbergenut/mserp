package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func verifyTeamDriverSetup(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	id := func(sql string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewFleetRepository(pool)
	dispatcher1 := id(`INSERT INTO dispatchers(full_name,normalized_name) VALUES('Setup Dispatcher One','setup dispatcher one') RETURNING id`)
	dispatcher2 := id(`INSERT INTO dispatchers(full_name,normalized_name) VALUES('Setup Dispatcher Two','setup dispatcher two') RETURNING id`)
	actor := id(`INSERT INTO app_users(username,password_hash) VALUES('Team setup',crypt('test-only',gen_salt('bf'))) RETURNING id`)
	hired, _ := time.Parse(time.DateOnly, "2026-09-24")
	main1, err := repo.CreateDriver(ctx, DriverInput{FullName: "Setup Main One", PayType: "cpm", PayRate: 0.7, Active: true, DispatcherID: &dispatcher1, AssignmentWeek: "2026-09-21", EscrowAmount: "0"})
	if err != nil {
		t.Fatal(err)
	}
	main2, err := repo.CreateDriver(ctx, DriverInput{FullName: "Setup Main Two", PayType: "cpm", PayRate: 0.8, Active: true, DispatcherID: &dispatcher2, AssignmentWeek: "2026-09-21", EscrowAmount: "0"})
	if err != nil {
		t.Fatal(err)
	}
	truck1, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "SETUP-1", Active: true, Status: "assigned", DriverID: &main1.ID, AssignmentWeek: "2026-09-21"})
	if err != nil {
		t.Fatal(err)
	}
	truck2, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "SETUP-2", Active: true, Status: "assigned", DriverID: &main2.ID, AssignmentWeek: "2026-09-21"})
	if err != nil {
		t.Fatal(err)
	}
	mode := true
	input := DriverInput{FullName: "Setup Secondary", IsTeamDriver: &mode, Active: true, TruckID: &truck1.ID, DispatcherID: &dispatcher2, PayType: "gross_percentage", PayRate: 95, HireDate: &hired, EscrowAmount: "999", ChargeActor: actor, AssignmentWeek: "2026-09-28"}
	secondary, err := repo.CreateDriver(ctx, input)
	if err != nil {
		t.Fatal("atomic team creation", err)
	}
	if secondary.TeamRole != "team" || secondary.TruckID != nil || secondary.TeamTruckID == nil || *secondary.TeamTruckID != truck1.ID || secondary.DispatcherID == nil || *secondary.DispatcherID != dispatcher1 || secondary.PayRate != 0 {
		t.Fatal("incorrect shared profile", secondary)
	}
	if id(`SELECT amount::text FROM driver_escrows WHERE driver_id=$1`, secondary.ID) != "0.00" {
		t.Fatal("new team driver acquired escrow")
	}
	team, err := repo.GetDriverTeam(ctx, secondary.ID)
	if err != nil || len(team.Periods) != 1 || team.Periods[0].StartWeek != "2026-09-21" {
		t.Fatal("hire date did not select the team Monday", team, err)
	}
	if got, err := repo.GetTruck(ctx, truck1.ID); err != nil || got.DriverID == nil || *got.DriverID != main1.ID {
		t.Fatal("team create displaced main", got, err)
	}
	input.FullName = "Blocked team creation"
	if _, err = repo.CreateDriver(ctx, input); err == nil {
		t.Fatal("second team member accepted")
	}
	if id(`SELECT count(*)::text FROM drivers WHERE normalized_name='blocked team creation'`) != "0" {
		t.Fatal("failed team setup left a profile")
	}
	input.FullName = secondary.FullName
	input.AssignmentWeek = "2026-09-28"
	input.TruckID = &truck2.ID
	input.TeamVersion = &secondary.TeamVersion
	moved, err := repo.UpdateDriver(ctx, secondary.ID, input)
	if err != nil {
		t.Fatal("move team from secondary profile", err)
	}
	if moved.TeamPartnerID == nil || *moved.TeamPartnerID != main2.ID || moved.DispatcherID == nil || *moved.DispatcherID != dispatcher2 {
		t.Fatal("move did not inherit dispatcher", moved)
	}
	old, err := repo.GetDriverTeam(ctx, main1.ID)
	if err != nil || old.Periods[0].EndWeek == nil || *old.Periods[0].EndWeek != "2026-09-28" {
		t.Fatal("prior main team was not closed", old, err)
	}
	if _, err = repo.UpdateDriver(ctx, secondary.ID, input); !errors.Is(err, ErrDriverTeamConflict) {
		t.Fatal("stale secondary edit accepted", err)
	}
	mode = false
	input.TeamVersion = &moved.TeamVersion
	input.AssignmentWeek = "2026-10-05"
	input.TruckID = nil
	input.PayType = "cpm"
	input.PayRate = 0.8
	solo, err := repo.UpdateDriver(ctx, secondary.ID, input)
	if err != nil || solo.TeamRole != "" {
		t.Fatal("return to solo", solo, err)
	}
	// A pending FleetScope hire uses the same zero-rate, zero-escrow atomic path.
	intake := id(`INSERT INTO fleetscope_driver_intake(company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at)
 VALUES(gen_random_uuid(),gen_random_uuid(),'{"fullName":"Setup Intake Person","hireDate":"2026-10-05"}','setup intake person',now()) RETURNING id`)
	mode = true
	intakeDate, _ := time.Parse(time.DateOnly, "2026-10-05")
	input.FullName = "Setup Intake Person"
	input.HireDate = &intakeDate
	input.TruckID = &truck2.ID
	input.TeamVersion = nil
	completed, err := repo.CompleteDriverIntake(ctx, intake, actor, "", &input, false)
	if err != nil {
		t.Fatal("team intake completion", err)
	}
	if completed.TeamRole != "team" || completed.PayRate != 0 || completed.DispatcherID == nil || *completed.DispatcherID != dispatcher2 {
		t.Fatal("team intake profile", completed)
	}
	if id(`SELECT driver_id::text FROM fleetscope_driver_intake WHERE id=$1 AND completed_at IS NOT NULL`, intake) != completed.ID {
		t.Fatal("intake did not complete atomically")
	}
	blockedIntake := id(`INSERT INTO fleetscope_driver_intake(company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at)
 VALUES(gen_random_uuid(),gen_random_uuid(),'{"fullName":"Blocked Intake"}','blocked intake',now()) RETURNING id`)
	input.FullName = "Blocked Intake"
	if _, err = repo.CompleteDriverIntake(ctx, blockedIntake, actor, "", &input, false); err == nil {
		t.Fatal("occupied truck intake accepted")
	}
	if id(`SELECT count(*)::text FROM fleetscope_driver_intake WHERE id=$1 AND completed_at IS NULL AND driver_id IS NULL`, blockedIntake) != "1" {
		t.Fatal("failed intake was completed")
	}
	if id(`SELECT count(*)::text FROM drivers WHERE normalized_name='blocked intake'`) != "0" {
		t.Fatal("failed intake left a driver")
	}
	// Current details inherit subsequent dispatcher changes without touching the
	// person's dated solo assignment history or owning a second truck assignment.
	exec(`UPDATE drivers SET dispatcher_id=$2 WHERE id=$1`, main2.ID, dispatcher1)
	current, err := repo.GetDriver(ctx, completed.ID)
	if err != nil || current.DispatcherID == nil || *current.DispatcherID != dispatcher1 {
		t.Fatal("shared dispatcher stopped following main", current, err)
	}
	futureDate, _ := chargeWeek(ChargeCurrentWeek())
	futureDate = futureDate.AddDate(0, 0, 7)
	input.FullName = "Future Team Newcomer"
	input.HireDate = &futureDate
	input.TruckID = &truck1.ID
	input.TeamVersion = nil
	future, err := repo.CreateDriver(ctx, input)
	if err != nil || future.TeamRole != "team" || future.TeamStartWeek == nil || *future.TeamStartWeek != futureDate.Format(time.DateOnly) {
		t.Fatal("scheduled team is not editable before its start", future, err)
	}
}
