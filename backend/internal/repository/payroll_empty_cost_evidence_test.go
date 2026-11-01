package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollEmptyCostEvidenceDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		for phase := 0; phase < 2; phase++ {
			if phase == 1 {
				// A zero-valued transaction remains original evidence. An empty
				// total is not proof that the transaction scope is empty.
				if _, err := pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES($1,'2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',0,repeat('e',64))`, projectionTruck); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			read := payrollRoutingFactsContext(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), snapshot)
			for _, kind := range []string{"driver", "investor"} {
				id := projectionDriver
				if kind == "investor" {
					id = projectionTruck
				}
				// Unknown carried debt and a differing frozen total must remain
				// explicit even when the native source scope is known to be empty.
				card := DriverPayDriver{ID: id, FuelTotal: "50.00", TollTotal: "20.00", Edits: DriverPayEdits{Costs: &DriverPayCosts{FuelCarry: "30.00", FuelDue: "80.00", TollCarry: "10.00", TollDue: "30.00"}}}
				for _, cost := range []string{"fuel", "toll"} {
					want, err := preparedCostEvidence(ctx, tx, kind, id, "2026-10-05", cost, card)
					if err != nil {
						t.Fatal(err)
					}
					got, err := preparedCostEvidence(read, tx, kind, id, "2026-10-05", cost, card)
					if err != nil || !reflect.DeepEqual(got, want) || got.Notice == "" {
						t.Fatal("empty-source evidence differs", phase, kind, cost, got, want, err)
					}
					if phase == 1 && kind == "investor" && cost == "toll" && len(got.Transactions) != 2 {
						t.Fatal("zero source transaction was omitted", got)
					}
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	})
}
