package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayDispatcherNameComparison(t *testing.T) {
	for _, tc := range []struct {
		provider, native string
		warn, similar    bool
	}{
		{"  JOSÉ   RAMÍREZ ", "Jose Ramirez", false, false},
		{"", "Jose Ramirez", false, false},
		{"Jose Ramirez", "Unassigned", false, false},
		{"Ramirez, Jose", "Jose Ramirez", true, true},
		{"Jose Ramirez", "Jose Luis Ramirez", true, true},
		{"J. Ramirez", "Jose Ramirez", true, true},
		{"Jose Ramíres", "Jose Ramirez", true, true},
		{"Carlos Ramirez", "Jose Ramirez", true, false},
	} {
		t.Run(tc.provider+" / "+tc.native, func(t *testing.T) {
			got := comparePayDispatcherNames(tc.provider, tc.native)
			if (got != nil) != tc.warn || (got != nil && got.Similar != tc.similar) {
				t.Fatalf("comparison = %+v, want warning %t, similar %t", got, tc.warn, tc.similar)
			}
		})
	}
}

func TestPayrollDispatcherComparisonDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		_, err := pool.Exec(ctx, `DELETE FROM driver_dispatcher_assignments WHERE driver_id='`+projectionDriver+`';
 INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_name,assigned_at,unassigned_at) VALUES
 ('`+projectionDriver+`','Jose Ramirez','2026-10-01 00:00-04','2026-10-06 00:00-04'),
 ('`+projectionDriver+`','Current Dispatcher','2026-10-06 00:00-04',NULL);
 UPDATE loads SET dispatcher_name='J. Ramirez' WHERE id=18201`)
		if err != nil {
			t.Fatal(err)
		}
		week, err := time.Parse(time.DateOnly, "2026-10-05")
		if err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		for kind, id := range map[string]string{"driver": projectionDriver, "investor": projectionTruck} {
			check := func(mode string, result DriverPayWeek, err error) {
				t.Helper()
				if err != nil || len(result.Drivers) != 1 || len(result.Drivers[0].Loads) != 1 {
					t.Fatalf("%s %s: expected one statement with one load, got %+v, %v", mode, kind, result, err)
				}
				got := result.Drivers[0].Loads[0].DispatcherComparison
				if got == nil || *got != (PayDispatcherComparison{DataTruck: "J. Ramirez", Clustie: "Jose Ramirez", Similar: true}) {
					t.Fatalf("%s %s: unexpected dispatcher comparison %+v", mode, kind, got)
				}
			}
			result, err := repo.PayrollStatement(ctx, kind, week, id)
			check("calculated", result, err)
			result, err = repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, false)
			check("prepared", result, err)
		}
	})
}
