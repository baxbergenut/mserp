package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServiceProgramCalendar(t *testing.T) {
	for _, tc := range []struct {
		date   string
		months int
		want   string
	}{{"2026-01-31", 1, "2026-02-28"}, {"2024-01-31", 1, "2024-02-29"}, {"2026-08-31", 6, "2027-02-28"}} {
		day, _ := time.Parse(time.DateOnly, tc.date)
		if got := addServiceMonths(day, tc.months).Format(time.DateOnly); got != tc.want {
			t.Fatalf("%s + %d: %s", tc.date, tc.months, got)
		}
	}
	last := "2026-01-31"
	months := 1
	interval := 20000
	baseline := 1000
	current := 1100
	date := "2026-02-28"
	d := MaintenanceDue{LastDate: &last, LastMiles: &baseline, CurrentMiles: &current, CurrentDate: &date, Schedule: &MaintenanceSchedule{Enabled: true, IntervalMiles: &interval, IntervalMonths: &months}}
	calculateMaintenanceDue(&d, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	if d.Status != "due" {
		t.Fatal("calendar limit must win", d)
	}
}

// Runs for both the fresh schema and the pre-maintenance migration replay, as
// mserp_app. Keep this at the end of the existing maintenance database fixture.
func testServicePrograms(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	repo := NewMaintenanceRepository(pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var truck, other string
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('PROGRAM-TEST') RETURNING id::text`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('PROGRAM-OTHER') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	interval := 50000
	p, err := repo.SaveProgram(ctx, ServiceProgram{Name: "Clutch check 50k", IntervalMiles: &interval, WarningMiles: 1000, Active: true, UseDefaultAssignees: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	// A linked reporting type completes its program without a checkbox, even
	// for a truck outside the program. Recording work never adds enrollment.
	linkedType, err := repo.SaveType(ctx, MaintenanceType{Code: "clutch_inspection", Name: "Clutch inspection", Active: true, ProgramID: &p.ID})
	if err != nil {
		t.Fatal(err)
	}
	var unassigned, autoID string
	if err = admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('AUTO-UNASSIGNED') RETURNING id::text`).Scan(&unassigned); err != nil {
		t.Fatal(err)
	}
	pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&autoID)
	autoDate, _ := time.Parse(time.DateOnly, "2026-10-01")
	automatic, err := repo.Save(ctx, MaintenanceInput{ID: autoID, TruckID: &unassigned, ProgramIDs: []string{}, ServiceDate: &autoDate, Status: "completed", Location: "internal", Services: []MaintenanceWork{{TypeCode: linkedType.Code}}, Actor: "Tester"})
	if err != nil || len(automatic.ProgramIDs) != 1 || automatic.ProgramIDs[0] != p.ID {
		t.Fatal("type did not complete program", automatic, err)
	}
	if values, err := repo.ProgramTrucks(ctx, p.ID, unassigned); err != nil || len(values) != 0 {
		t.Fatal("work order enrolled an unassigned truck", values, err)
	}
	// Older type editors omit the new field; renames preserve the saved link.
	linkedType.ProgramID = nil
	linkedType.Name = "Clutch inspection renamed"
	linkedType, err = repo.SaveType(ctx, linkedType)
	if err != nil || linkedType.ProgramID == nil || *linkedType.ProgramID != p.ID {
		t.Fatal("rename lost program link", linkedType, err)
	}
	stale := p
	// Simulate an older program editor that omits the new mapping controls.
	p.ServiceTypeCodes = nil
	p.Instructions = "Inspect clutch wear"
	p, err = repo.SaveProgram(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveProgram(ctx, stale, ""); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale program accepted", err)
	}
	members := []ProgramTruckInput{{TruckID: truck, Active: true}, {TruckID: other, Active: true}}
	if err = repo.SaveProgramTrucks(ctx, p.ID, members, ""); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProgramTrucks(ctx, p.ID, members, ""); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale membership accepted", err)
	}
	allRows, e := repo.ProgramTrucks(ctx, p.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	for i, want := range allRows {
		workspace, e := repo.ProgramWorkspace(ctx, p.ID, "", "", "", Pagination{Page: i + 1, PageSize: 1})
		if e != nil || workspace.Trucks.Total != len(allRows) || len(workspace.Trucks.Items) != 1 || !reflect.DeepEqual(workspace.Trucks.Items[0], want) {
			t.Fatal("workspace pagination changed due evidence", workspace, e)
		}
	}
	values, err := repo.ProgramTrucks(ctx, p.ID, truck)
	if err != nil || len(values) != 1 || values[0].Status != "missing_information" || values[0].LastDate != nil {
		t.Fatal("enrollment invented completion", values, err)
	}
	for _, tc := range []struct{ stored, want string }{{"maintenance", "shop"}, {"out_of_service", "out_of_service"}, {"available", "ready"}} {
		exec(`UPDATE trucks SET status=$2,status_reason=$2 || ' test' WHERE id=$1`, truck, tc.stored)
		got, err := repo.ProgramTrucks(ctx, p.ID, truck)
		if err != nil || len(got) != 1 || got[0].TruckOperatingStatus != tc.want {
			t.Fatalf("program truck status: got %+v, want %s: %v", got, tc.want, err)
		}
	}
	if err = repo.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: truck, Active: true}}, ""); err != nil {
		t.Fatal(err)
	}
	// All completions are explicit and have no invoice; one truck can follow both.
	service := func(programs []string, date string, mileage int) MaintenanceRecord {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		day, _ := time.Parse(time.DateOnly, date)
		v, e := repo.Save(ctx, MaintenanceInput{ID: id, ProgramIDs: programs, TruckID: &truck, ServiceDate: &day, Odometer: &mileage, Status: "completed", Location: "internal", Services: []MaintenanceWork{{TypeCode: "other"}}, Actor: "Tester"})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	first := service([]string{p.ID}, "2026-10-01", 100000)
	service([]string{OilChangeProgramID}, "2026-10-02", 110000)
	values, err = repo.ProgramTrucks(ctx, p.ID, truck)
	if err != nil || *values[0].LastRecordID != first.ID || *values[0].LastMiles != 100000 || *values[0].MilesSinceService != 10000 {
		t.Fatal("oil completion reset clutch or mileage difference incorrect", values, err)
	}
	// Correcting a baseline is audited and cannot overwrite a concurrent save.
	v := values[0]
	base := 105000
	input := MaintenancePMInput{TruckID: truck, LastDate: "2026-10-01", LastMiles: &base, RecordID: v.LastRecordID, RecordVersion: v.RecordVersion, ActorName: "Tester"}
	if err = repo.SaveProgramBaseline(ctx, p.ID, input, v.MemberVersion); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProgramBaseline(ctx, p.ID, input, v.MemberVersion); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale baseline accepted", err)
	}
	values, err = repo.ProgramTrucks(ctx, p.ID, truck)
	if err != nil || *values[0].LastMiles != base {
		t.Fatal(values, err)
	}
	combined := service([]string{p.ID, OilChangeProgramID}, "2026-10-03", 120000)
	for _, program := range []string{p.ID, OilChangeProgramID} {
		values, err = repo.ProgramTrucks(ctx, program, truck)
		if err != nil || *values[0].LastRecordID != combined.ID || *values[0].LastMiles != 120000 {
			t.Fatal("combined completion failed", values, err)
		}
	}
	if err = repo.Void(ctx, combined.ID, combined.Version, "Synthetic correction", "Tester"); err != nil {
		t.Fatal(err)
	}
	values, err = repo.ProgramTrucks(ctx, p.ID, truck)
	if err != nil || *values[0].LastMiles != 105000 {
		t.Fatal("void did not restore prior effective baseline", values, err)
	}
	// Refreshes are idempotent; missing telemetry retains the known reminder.
	live := map[string]MaintenanceReading{truck: {Date: "2026-10-21", Mileage: 154500}}
	today := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err = repo.RefreshMaintenanceTasks(ctx, today, live); err != nil {
			t.Fatal(err)
		}
	}
	var open int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM system_task_records WHERE maintenance_program_id=$1 AND maintenance_truck_id=$2 AND completed_at IS NULL`, p.ID, truck).Scan(&open); err != nil || open != 1 {
		t.Fatal("duplicate or missing reminder", open, err)
	}
	if err = repo.RefreshMaintenanceTasks(ctx, today, nil); err != nil {
		t.Fatal(err)
	}
	// Specific assignees must control both task visibility and sidebar counts.
	var user, role string
	if err = admin.QueryRow(ctx, `SELECT id::text FROM app_roles WHERE system_role LIMIT 1`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) VALUES('Program Reviewer','program-reviewer@example.test',crypt('synthetic-password',gen_salt('bf')),$1) RETURNING id::text`, role).Scan(&user); err != nil {
		t.Fatal(err)
	}
	p.UseDefaultAssignees = false
	p.AssigneeRoleIDs = []string{role}
	p, err = repo.SaveProgram(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}
	tasks := NewCustomTaskRepository(pool)
	viewer := WithTaskViewer(ctx, user, false)
	board, err := tasks.Board(viewer, Pagination{Page: 1, PageSize: 100}, p.Name, "open", true, true)
	if err != nil || board.Total != 1 {
		t.Fatal("program role visibility failed", board, err)
	}
	p.AssigneeRoleIDs = []string{}
	p.AssigneeIDs = []string{}
	p, err = repo.SaveProgram(ctx, p, "")
	if err != nil {
		t.Fatal(err)
	}
	board, err = tasks.Board(viewer, Pagination{Page: 1, PageSize: 100}, p.Name, "open", true, true)
	if err != nil || board.Total != 0 {
		t.Fatal("unassigned program leaked", board, err)
	}
	values, err = repo.ProgramTrucks(ctx, p.ID, truck)
	if err != nil {
		t.Fatal(err)
	}
	v = values[0]
	if err = repo.SaveProgramTrucks(ctx, p.ID, []ProgramTruckInput{{TruckID: truck, Version: v.MemberVersion, Active: false}}, ""); err != nil {
		t.Fatal(err)
	}
	if err = repo.RefreshMaintenanceTasks(ctx, today, live); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM system_task_records WHERE maintenance_program_id=$1 AND maintenance_truck_id=$2 AND completed_at IS NULL`, p.ID, truck).Scan(&open); err != nil || open != 0 {
		t.Fatal("removed membership retained reminder", open, err)
	}
	var history int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM maintenance_records WHERE id=$1`, first.ID).Scan(&history); err != nil || history != 1 {
		t.Fatal("removal deleted history", history, err)
	}
	// Paused programs do not silently enroll or clear service history.
	p.Active = false
	if _, err = repo.SaveProgram(ctx, p, ""); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE trucks SET active=false WHERE id=ANY($1::uuid[])`, []string{truck, other})
	testProgramServiceTypeSettings(t, ctx, pool)
}

func testProgramServiceTypeSettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	repo := NewMaintenanceRepository(pool)
	interval := 25000
	a, err := repo.SaveType(ctx, MaintenanceType{Code: "reset_a", Name: "Reset A", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.SaveType(ctx, MaintenanceType{Code: "reset_b", Name: "Reset B", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.SaveProgram(ctx, ServiceProgram{Name: "Multiple resetting types", IntervalMiles: &interval, Active: true, ServiceTypeCodes: []string{a.Code, b.Code}, ServiceTypeVersions: map[string]int{a.Code: a.Version, b.Code: b.Version}}, "")
	if err != nil || len(p.ServiceTypeCodes) != 2 || p.ServiceTypeVersions[a.Code] != a.Version+1 {
		t.Fatal("program mapping creation", p, err)
	}
	// Saving through the type editor invalidates an open program editor too.
	a.Version = p.ServiceTypeVersions[a.Code]
	a.Name = "Reset A renamed"
	a, err = repo.SaveType(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	stale := p
	stale.Name = "Must roll back"
	stale.ServiceTypeCodes = []string{}
	if _, err = repo.SaveProgram(ctx, stale, ""); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale mapping edit accepted", err)
	}
	programs, err := repo.Programs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range programs {
		if current.ID == p.ID {
			p = current
		}
	}
	if p.Name != "Multiple resetting types" || len(p.ServiceTypeCodes) != 2 {
		t.Fatal("partial save on conflict", p)
	}
	// The same type cannot be silently taken from another program.
	if _, err = repo.SaveProgram(ctx, ServiceProgram{Name: "Cannot steal", IntervalMiles: &interval, ServiceTypeCodes: []string{a.Code}, ServiceTypeVersions: map[string]int{a.Code: a.Version}}, ""); !errors.Is(err, ErrMaintenanceInvalid) {
		t.Fatal("mapping stolen", err)
	}
	p.ServiceTypeCodes = []string{b.Code}
	p, err = repo.SaveProgram(ctx, p, "")
	if err != nil || len(p.ServiceTypeCodes) != 1 || p.ServiceTypeCodes[0] != b.Code {
		t.Fatal("unlink one resetting type", p, err)
	}
	types, err := repo.Types(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range types {
		if current.Code == a.Code && (current.ProgramID != nil || current.Version != a.Version+1) {
			t.Fatal("type editor did not reflect program unlink", current)
		}
	}
	p.ServiceTypeCodes = []string{}
	p, err = repo.SaveProgram(ctx, p, "")
	if err != nil || len(p.ServiceTypeCodes) != 0 {
		t.Fatal("clear resetting types", p, err)
	}
}
