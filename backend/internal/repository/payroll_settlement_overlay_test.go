package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollSettlementOverlayDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const second = "18200000-0000-0000-0000-000000001010"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Second Driver','second driver','cpm',0.65)`, second); err != nil {
			t.Fatal(err)
		}
		old := DriverPayDriver{ID: projectionDriver, FullName: "Projection Driver", BrokerReimbursements: []BrokerReimbursementReminder{{ExpenseID: "old-reminder", Amount: "10.00"}}}
		newlyFinalized := DriverPayDriver{ID: second, FullName: "Second Driver"}
		for _, row := range []DriverPayDriver{old, newlyFinalized} {
			body, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05',$2)`, row.ID, body); err != nil {
				t.Fatal(err)
			}
		}
		current := old
		current.BrokerReimbursements = []BrokerReimbursementReminder{{ExpenseID: "current-reminder", Amount: "25.00"}}
		current.Settlement = &PayrollSettlement{Finalized: true, Version: 1}
		before, _ := json.Marshal(current)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		result, err := overlaySelectedPayrollSettlements(ctx, tx, DriverPayWeek{WeekStart: "2026-10-05", Drivers: []DriverPayDriver{current, newlyFinalized}}, map[string]bool{second: true})
		if err != nil || len(result.Drivers) != 2 {
			t.Fatal(result, err)
		}
		after, _ := json.Marshal(result.Drivers[0])
		if string(before) != string(after) {
			t.Fatalf("finalizing another row replaced current display on an existing settlement\n%s\n%s", before, after)
		}
		if result.Drivers[1].Settlement == nil || !result.Drivers[1].Settlement.Finalized {
			t.Fatal("newly finalized row did not receive its committed settlement metadata")
		}
	})
}
