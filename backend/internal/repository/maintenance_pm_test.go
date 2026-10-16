package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func testMaintenancePMMigration(t *testing.T, ctx context.Context, db *pgx.Conn, migration string) {
	t.Helper()
	exec := func(sql string) {
		t.Helper()
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO trucks(unit_number) VALUES('PM-MERGE'),('OIL-ONLY');
INSERT INTO maintenance_schedules(service_code,interval_miles) VALUES('pm',5000),('oil',10000);
INSERT INTO maintenance_schedules(truck_id,service_code,interval_miles)
SELECT id,'oil',7000 FROM trucks WHERE unit_number='OIL-ONLY';
INSERT INTO app_users(username,password_hash) VALUES('PM assignee',crypt('synthetic-test',gen_salt('bf'))),('Oil assignee',crypt('synthetic-test',gen_salt('bf')));
UPDATE system_task_assignments SET assignee_ids=ARRAY[(SELECT id FROM app_users WHERE username='PM assignee')] WHERE kind='maintenance_pm';
UPDATE system_task_assignments SET assignee_ids=ARRAY[(SELECT id FROM app_users WHERE username='Oil assignee')],assignee_role_ids=ARRAY[(SELECT id FROM app_roles WHERE system_role)] WHERE kind='maintenance_oil';
INSERT INTO system_task_records(id,kind,title,created_at,maintenance_truck_id,maintenance_service,maintenance_observed_miles)
SELECT gen_random_uuid(),'maintenance_'||s.code,s.title,now(),t.id,s.code,s.miles
FROM trucks t CROSS JOIN (VALUES('pm','PM service due',1100),('oil','Oil change overdue',1200)) s(code,title,miles) WHERE t.unit_number='PM-MERGE';`)
	exec(migration)
	var pmMiles, oilOnlyMiles, assignees, roles, open, total, strongest, retired int
	err := db.QueryRow(ctx, `SELECT
 (SELECT interval_miles FROM maintenance_schedules WHERE truck_id IS NULL),
 (SELECT interval_miles FROM maintenance_schedules s JOIN trucks t ON t.id=s.truck_id WHERE unit_number='OIL-ONLY'),
 (SELECT cardinality(assignee_ids) FROM system_task_assignments WHERE kind='maintenance_pm'),
 (SELECT cardinality(assignee_role_ids) FROM system_task_assignments WHERE kind='maintenance_pm'),
 (SELECT count(*) FROM system_task_records WHERE completed_at IS NULL),
 (SELECT count(*) FROM system_task_records),
 (SELECT maintenance_observed_miles FROM system_task_records WHERE completed_at IS NULL),
 (SELECT count(*) FROM system_task_assignments WHERE kind='maintenance_oil')`).Scan(&pmMiles, &oilOnlyMiles, &assignees, &roles, &open, &total, &strongest, &retired)
	if err != nil || pmMiles != 5000 || oilOnlyMiles != 7000 || assignees != 2 || roles != 1 || open != 1 || total != 2 || strongest != 1200 || retired != 0 {
		t.Fatal("PM consolidation lost configuration or reminder history", pmMiles, oilOnlyMiles, assignees, roles, open, total, strongest, retired, err)
	}
	var audit int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM access_audit WHERE action='maintenance_pm_consolidation'`).Scan(&audit); err != nil || audit != 5 {
		t.Fatal("missing prior configuration audit", audit, err)
	}
	// The rest of the shared suite starts without this fixture's defaults/tasks.
	exec(`DELETE FROM maintenance_schedules;
DELETE FROM system_task_records;
UPDATE system_task_assignments SET assignee_ids='{}',assignee_id=NULL,assignee_role_ids='{}' WHERE kind='maintenance_pm';
UPDATE trucks SET active=false WHERE unit_number IN ('PM-MERGE','OIL-ONLY');`)
}
