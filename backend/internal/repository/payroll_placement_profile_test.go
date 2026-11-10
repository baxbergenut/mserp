package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

// Opt-in production-sized diagnostic, never a production mutation.
func profilePayrollPlacementCommands(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverPayRepository, actor string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE loads SET status='dispatched' WHERE id=17128`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		worked, err := repo.PrepareNextPayrollWeek(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	week, _ := chargeWeek("2026-10-19")
	for _, kind := range []string{"driver", "investor"} {
		source := "driver171"
		if kind == "investor" {
			source = "truck171"
		}
		var id string
		if err := pool.QueryRow(ctx, `SELECT md5($1)::uuid::text`, source).Scan(&id); err != nil {
			t.Fatal(err)
		}
		for index, action := range []string{"remove", "restore"} {
			page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
			if err != nil || len(page.Drivers) != 1 {
				t.Fatal(kind, action, err)
			}
			card := page.Drivers[0]
			candidates := card.Loads
			if action == "restore" {
				candidates = card.RemovedLoads
			}
			var chosen *DriverPayLoad
			for i := range candidates {
				if candidates[i].LoadRecordID != nil && *candidates[i].LoadRecordID == 17128 {
					chosen = &candidates[i]
					break
				}
			}
			if chosen == nil {
				t.Fatal("profile load absent", kind, action)
			}
			in := PayLoadPlacementInput{PayrollCommandMetadata: PayrollCommandMetadata{RequestID: fmt.Sprintf("18200000-0000-0000-0001-%012d", index+1+map[string]int{"driver": 0, "investor": 10}[kind]), ExpectedStatementRevision: card.StatementRevision, StatementKind: kind, StatementID: id, StatementWeekStart: page.WeekStart}, DriverID: id, WeekStart: page.WeekStart, Action: action, LoadRecordID: 17128, LoadTripID: chosen.LoadTripID, CommentKey: chosen.CommentKey, BoardVersion: chosen.BoardVersion, PlacementVersion: chosen.PayrollPlacementVersion}
			measured, metrics := db.WithQueryMetrics(ctx)
			start := time.Now()
			err = repo.PlacePayLoad(measured, kind, in, actor)
			if err != nil {
				t.Fatal(kind, action, err)
			}
			phases, queries := metrics.Details()
			body, _ := json.Marshal(map[string]any{"kind": kind, "action": action, "ms": float64(time.Since(start).Microseconds()) / 1000, "calls": metrics.Queries.Load(), "events": metrics.Events(), "phases": phases, "queries": queries})
			t.Logf("placement command diagnostic %s", body)
		}
	}
}
