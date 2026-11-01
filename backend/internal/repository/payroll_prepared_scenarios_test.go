package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reuse established business-rule fixtures with prepared settlement validation;
// their explicit amount, dated routing, guard and frozen-history assertions stay
// intact. Preparation does not replace any canonical writes in those fixtures.
func TestPreparedPayrollTeamsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		verifyDriverTeams(t, ctx, pool, true)
	})
}

func TestPreparedPayrollOwnerDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Prepared owner fixture',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		verifyTruckRecurring(t, ctx, pool, actor, true)
	})
}

func TestPreparedPayrollExpenseWindowDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		testExpenseCollectionWindow(t, ctx, admin, pool, true)
	})
}

func settlePayrollFixture(t *testing.T, ctx context.Context, repo *DriverPayRepository, kind string, week time.Time, id, revision, actor, reason string, reopen, override, prepared bool) (DriverPayWeek, error) {
	t.Helper()
	var options []PayrollSettlementCommand
	if prepared {
		ctx = WithPayrollReceipt(ctx)
		repo.ConfigurePreparedFinalization(true)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if err := repo.RequestPayrollPreparation(ctx, week, true); err != nil {
			t.Fatal(err)
		}
		for {
			worked, err := repo.PrepareNextPayrollWeek(ctx)
			if err != nil || !worked {
				t.Fatal("prepare business-rule fixture", worked, err)
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{ID: id})
			if err != nil {
				t.Fatal(err)
			}
			if page.State != "ready" {
				continue
			}
			version := page.Statements[id].Revision
			if id == "" {
				version = page.Generation
			}
			var request string
			if err = repo.pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&request); err != nil {
				t.Fatal(err)
			}
			options = []PayrollSettlementCommand{{RequestID: request, ExpectedStatementRevision: version}}
			revision = page.Report.Revision
			break
		}
	}
	if kind == "investor" {
		return repo.SettleInvestor(ctx, week, id, revision, actor, reason, reopen, override, options...)
	}
	return repo.Settle(ctx, week, id, revision, actor, reason, reopen, override, options...)
}
