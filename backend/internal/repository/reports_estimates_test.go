package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportsEstimatedRecurringDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		exec := func(sql string) {
			t.Helper()
			if _, err := admin.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		fixture, err := os.ReadFile("testdata/reports_closed_week.sql")
		if err != nil {
			t.Fatal(err)
		}
		exec(string(fixture))
		exec(`UPDATE payroll_settlements SET finalized=false;
 UPDATE drivers SET first_name='Mary Jane',last_name='Driver',full_name='Mary Jane Driver' WHERE id='61000000-0000-0000-0000-000000000002';
 INSERT INTO driver_charge_types(id,name,direction,amount,amounts,eligibility) VALUES
 ('63000000-0000-0000-0000-000000000001','Weekly insurance','charge',999,ARRAY[999]::numeric[],'calendar'),
 ('63000000-0000-0000-0000-000000000002','Credit','reimbursement',999,ARRAY[999]::numeric[],'calendar'),
 ('63000000-0000-0000-0000-000000000003','Paused','charge',999,ARRAY[999]::numeric[],'calendar'),
 ('63000000-0000-0000-0000-000000000004','Idle fee','charge',999,ARRAY[999]::numeric[],'no_loads');
 INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included)
 SELECT '61000000-0000-0000-0000-000000000005',id,'2026-10-05',250,true FROM driver_charge_types WHERE id::text LIKE '63000000-%';
 INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included) VALUES
 ('61000000-0000-0000-0000-000000000005','63000000-0000-0000-0000-000000000003','2026-10-12',300,false),
 ('61000000-0000-0000-0000-000000000005','63000000-0000-0000-0000-000000000001','2026-10-19',900,true);`)
		period, _ := ResolveReportsPeriod("2026-10-12", "2026-10-18", "", "", time.Now())
		repo := NewReportsRepository(pool)
		read := func(rc, profit float64, estimate bool) ReportRow {
			t.Helper()
			result, err := repo.Read(ctx, ReportsQuery{View: ReportOperator, Period: period, CurrentOnly: true})
			if err != nil || len(result.Current) != 1 {
				t.Fatal(result, err)
			}
			row := result.Current[0]
			if row.Metrics["recurringCharges"] != rc || row.Metrics["contribution"] != profit || row.Estimated["recurringCharges"] != estimate {
				t.Fatalf("recurring/profit estimate: %+v", row)
			}
			if len(row.AssignedDrivers) != 1 || row.AssignedDrivers[0].Name != "Mary Jane" {
				t.Fatal("recorded first name", row.AssignedDrivers)
			}
			var sum float64
			for _, source := range row.Sources {
				sum += source.Values["recurringCharges"]
			}
			if sum != rc {
				t.Fatal("source reconciliation", sum, rc)
			}
			return row
		}
		row := read(25000, 54000, true)
		if !row.Estimated["carrierFees"] || !row.Estimated["contribution"] || row.Estimated["gross"] {
			t.Fatal("estimate propagation", row.Estimated)
		}
		exec(`UPDATE payroll_settlements SET finalized=true,report=jsonb_set(report,'{autoCharges}',
 '[{"name":"Weekly insurance","source":"truck_charge:61000000-0000-0000-0000-000000000005:63000000-0000-0000-0000-000000000001","amount":"-125.00"}]')
 WHERE driver_id='61000000-0000-0000-0000-000000000002'`)
		row = read(12500, 41500, false)
		if len(row.Estimated) != 0 {
			t.Fatal("frozen values still estimated", row.Estimated)
		}
		exec(`UPDATE payroll_settlements SET report=jsonb_set(report,'{autoCharges,0,amount}','"0.00"') WHERE driver_id='61000000-0000-0000-0000-000000000002'`)
		read(0, 29000, false)
		exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id='61000000-0000-0000-0000-000000000002'`)
		read(25000, 54000, true)
		exec(`UPDATE truck_charge_phases SET amount=180 WHERE type_id='63000000-0000-0000-0000-000000000001' AND week_start='2026-10-05'`)
		read(18000, 47000, true)
		// Latest excluded phase never resurrects an older amount, or a future one.
		exec(`UPDATE truck_charge_phases SET included=false WHERE type_id='63000000-0000-0000-0000-000000000001' AND week_start='2026-10-05'`)
		read(0, 29000, false)
	})
}
