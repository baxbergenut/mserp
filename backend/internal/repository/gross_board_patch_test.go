package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGrossBoardPatchPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		gross := NewGrossBoardRepository(pool)
		patch := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-05", DriverRate: "775", Fields: []string{"driverRate"}}
		saved, err := gross.SaveChanges(WithPayrollPublication(ctx), []GrossBoardEntry{patch}, "")
		if err != nil || len(saved) != 1 || saved[0].DriverRate != "775.00" || saved[0].LoadNumber != "PROJECTION-1" {
			t.Fatal(saved, err)
		}
		reference, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, reference, week, true)
		_ = reference.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			after, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(after.Report)
			if err != nil || after.State != "ready" || string(a) != string(b) {
				t.Fatal("field save broke atomic payroll publication", kind, err)
			}
		}
		if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_gross_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test publication failure'; END $$;
 CREATE TRIGGER fail_gross_publication BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION fail_gross_publication()`); err != nil {
			t.Fatal(err)
		}
		patch.DriverRate = "650"
		if _, err = gross.SaveChanges(WithPayrollPublication(ctx), []GrossBoardEntry{patch}, ""); err == nil {
			t.Fatal("publication failure acknowledged as saved")
		}
		var rate string
		if err = pool.QueryRow(ctx, `SELECT driver_rate::text FROM gross_board_entries WHERE driver_id=$1 AND service_date=$2`, projectionDriver, week).Scan(&rate); err != nil || rate != "775.00" {
			t.Fatal("failed publication did not roll back plan", rate, err)
		}
	})
}
