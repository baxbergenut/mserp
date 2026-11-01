package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollSettlementCommandDatabase(t *testing.T) {
	testPayrollSettlementCommands(t, false)
}
func TestPreparedPayrollSettlementDatabase(t *testing.T) {
	testPayrollSettlementCommands(t, true)
}
func testPayrollSettlementCommands(t *testing.T, prepared bool) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Settlement Command',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedFinalization(prepared)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		seq := 20
		for _, kind := range []string{"driver", "investor"} {
			for _, bulk := range []bool{false, true} {
				for _, reopen := range []bool{false, true} {
					seq++
					t.Run(fmt.Sprintf("%s/bulk=%t/reopen=%t", kind, bulk, reopen), func(t *testing.T) {
						versions := func() map[PayrollStatementIdentity]string {
							t.Helper()
							rows, err := pool.Query(ctx, `SELECT kind,statement_id::text,week_start::text,version_id::text FROM payroll_statement_state`)
							if err != nil {
								t.Fatal(err)
							}
							defer rows.Close()
							result := map[PayrollStatementIdentity]string{}
							for rows.Next() {
								var identity PayrollStatementIdentity
								var version string
								if err := rows.Scan(&identity.Kind, &identity.ID, &identity.WeekStart, &version); err != nil {
									t.Fatal(err)
								}
								result[identity] = version
							}
							if err := rows.Err(); err != nil {
								t.Fatal(err)
							}
							return result
						}
						beforeVersions := versions()
						page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{}, true)
						if err != nil {
							t.Fatal(err)
						}
						id := page.Drivers[0].ID
						expected := page.Drivers[0].StatementRevision
						if bulk {
							id = ""
							expected = page.Preparation.Generation
						}
						opt := PayrollSettlementCommand{RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", seq), ExpectedStatementRevision: expected}
						command := func() (DriverPayWeek, error) {
							if kind == "driver" {
								return repo.Settle(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Reviewed test reopening", reopen, !reopen, opt)
							}
							return repo.SettleInvestor(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Reviewed test reopening", reopen, !reopen, opt)
						}
						committed, err := command()
						if err != nil {
							t.Fatal(err)
						}
						if len(committed.AffectedStatements) == 0 || len(committed.Drivers) != 1 || (committed.Drivers[0].Settlement != nil && committed.Drivers[0].Settlement.Finalized) == reopen {
							t.Fatal("incomplete committed settlement", committed)
						}
						// A metadata-only freeze may preserve the opposite page. The
						// receipt must enumerate exactly the pointers actually changed.
						changed := map[PayrollStatementIdentity]bool{}
						for identity, version := range versions() {
							if beforeVersions[identity] != version {
								changed[identity] = true
							}
							delete(beforeVersions, identity)
						}
						for identity := range beforeVersions {
							changed[identity] = true
						}
						for _, identity := range committed.AffectedStatements {
							if !changed[identity] {
								t.Fatal("receipt contains unchanged or repeated statement", identity)
							}
							delete(changed, identity)
						}
						if len(changed) != 0 {
							t.Fatal("receipt omitted changed statements", changed)
						}
						for _, k := range []string{"driver", "investor"} {
							fresh, err := repo.PreparedPayroll(ctx, k, week, PayPageQuery{}, true)
							if err != nil || fresh.Preparation.State != "ready" {
								t.Fatal("success before publication", k, err)
							}
						}
						repo = NewDriverPayRepository(pool)
						repo.ConfigurePreparedFinalization(prepared)
						replayed, err := command()
						if err != nil {
							t.Fatal("settlement replay", err)
						}
						a, _ := json.Marshal(committed)
						b, _ := json.Marshal(replayed)
						if string(a) != string(b) {
							t.Fatal("settlement replay changed")
						}
					})
				}
			}
		}
	})
}

func TestPayrollSourceCommandDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Source Command',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE loads SET total_miles=450 WHERE id=18201`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		page, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil {
			t.Fatal(err)
		}
		row := page.Drivers[0]
		load := row.Loads[0]
		meta := PayrollCommandMetadata{RequestID: "18200000-0000-0000-0000-000000000031", ExpectedStatementRevision: row.StatementRevision, StatementKind: "driver", StatementID: row.ID, StatementWeekStart: page.WeekStart}
		input := PaySourceAcceptance{PayrollCommandMetadata: meta, Field: "totalMiles", DriverID: row.ID, Date: load.Date, Slot: load.Slot, Version: load.BoardVersion, LoadRecordID: *load.LoadRecordID, Miles: load.SystemMiles}
		ctx = WithPayrollPublication(ctx)
		undo, err := repo.AcceptPaySourceWithUndo(ctx, input, actor)
		if err != nil {
			t.Fatal(err)
		}
		if undo == "" {
			t.Fatal("missing undo receipt")
		}
		receipt, err := repo.PayrollSourceCommandReceipt(ctx, actor, meta.RequestID)
		if err != nil || receipt.UndoID != undo || receipt.RequestID != meta.RequestID || len(receipt.AffectedStatements) != 2 {
			t.Fatal("missing committed source identities", receipt, err)
		}
		if _, err = repo.PayrollSourceCommandReceipt(ctx, "18200000-0000-0000-0000-000000009999", meta.RequestID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("source receipt crossed actor boundary", err)
		}
		replay, err := NewDriverPayRepository(pool).AcceptPaySourceWithUndo(ctx, input, actor)
		if err != nil || undo != replay {
			t.Fatal("source acceptance replay", replay, err)
		}
		replayedReceipt, err := repo.PayrollSourceCommandReceipt(ctx, actor, meta.RequestID)
		beforeReceipt, _ := json.Marshal(receipt)
		afterReceipt, _ := json.Marshal(replayedReceipt)
		if err != nil || string(beforeReceipt) != string(afterReceipt) {
			t.Fatal("source replay changed committed identities", err)
		}
		fresh, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil || fresh.Preparation.State != "ready" || strings.TrimSuffix(fresh.Drivers[0].Loads[0].TotalMiles, ".00") != "450" {
			t.Fatal("source success before publication", fresh.Preparation, err)
		}
		meta.RequestID = "18200000-0000-0000-0000-000000000032"
		meta.ExpectedStatementRevision = fresh.Drivers[0].StatementRevision
		if err = repo.UndoPaySource(ctx, undo, actor, meta); err != nil {
			t.Fatal(err)
		}
		if err = repo.UndoPaySource(ctx, undo, actor, meta); err != nil {
			t.Fatal("undo replay", err)
		}
		restored, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil || restored.Preparation.State != "ready" || restored.Drivers[0].Loads[0].TotalMiles != row.Loads[0].TotalMiles {
			t.Fatal("undo publication", err)
		}
		// Related native writes use the same atomic boundary even without a pay
		// command response. This context is never installed on provider workers.
		tx, err := beginPayrollMutation(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `UPDATE gross_board_entries SET entered_miles=410,version=version+1 WHERE driver_id=$1::uuid`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		if err = commitPayrollMutation(ctx, tx); err != nil {
			t.Fatal(err)
		}
		related, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil || related.Preparation.State != "ready" || strings.TrimSuffix(related.Drivers[0].Loads[0].TotalMiles, ".00") != "410" {
			t.Fatal("related write publication", err)
		}
		var pending int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes WHERE dispatched_at IS NULL`).Scan(&pending); err != nil || pending != 0 {
			t.Fatal("committed command work requeued", pending, err)
		}
	})
}

func TestPayrollSaveCommandDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		repo.resourceLockAudit = true
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Command Test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='dual' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"driver", "investor"} {
			t.Run(kind, func(t *testing.T) {
				page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
				if err != nil || page.State != "ready" || len(page.Report.Drivers) != 1 {
					t.Fatal(page.State, err)
				}
				edits := page.Report.Drivers[0].Edits
				edits.ExpectedStatementRevision = page.Statements[edits.DriverID].Revision
				edits.RequestID = "18200000-0000-0000-0000-000000000010"
				if kind == "investor" {
					edits.RequestID = "18200000-0000-0000-0000-000000000011"
				}
				edits.Notes = "durable comment"
				save := func(e DriverPayEdits) (DriverPayEdits, error) {
					if kind == "investor" {
						return repo.SaveInvestorPay(ctx, e, actor)
					}
					return repo.Save(ctx, e, actor)
				}
				committed, err := save(edits)
				if err != nil {
					t.Fatal(err)
				}
				if committed.StatementRevision == "" || committed.StatementRevision == edits.ExpectedStatementRevision || committed.CommittedStatement == nil || len(committed.AffectedStatements) == 0 {
					t.Fatalf("missing committed result: %+v", committed)
				}
				for _, pageKind := range []string{"driver", "investor"} {
					got, err := repo.ReadPreparedPayrollPage(ctx, pageKind, week, PayPageQuery{})
					if err != nil || got.State != "ready" {
						t.Fatal("success returned before projections committed", pageKind, got.State, err)
					}
				}
				// Simulate a lost response and a process restart. The old edit version
				// and old displayed revision must recover the exact committed receipt.
				repo = NewDriverPayRepository(pool)
				replayed, err := save(edits)
				if err != nil {
					t.Fatal("lost response recovery", err)
				}
				a, _ := json.Marshal(committed)
				b, _ := json.Marshal(replayed)
				if string(a) != string(b) {
					t.Fatal("replay result changed")
				}
				changed := edits
				changed.Notes = "different content"
				if _, err = save(changed); !errors.Is(err, ErrDriverPayConflict) {
					t.Fatal("request identity reused", err)
				}
				changed.RequestID = "18200000-0000-0000-0000-000000000012"
				if _, err = save(changed); !errors.Is(err, ErrDriverPayConflict) {
					t.Fatal("old prepared revision accepted", err)
				}
				var n int
				if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_command_versions WHERE request_id=$1::uuid`, edits.RequestID).Scan(&n); err != nil || n != len(committed.AffectedStatements) {
					t.Fatal("receipt does not retain versions", n, err)
				}
				// Once ordinary collection writes have settled, a subsequent text save
				// must preserve unrelated versions and exactly match a fresh full report.
				before, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
				if err != nil {
					t.Fatal(err)
				}
				next := before.Report.Drivers[0].Edits
				next.RequestID = "18200000-0000-0000-0000-000000000021"
				if kind == "investor" {
					next.RequestID = "18200000-0000-0000-0000-000000000022"
				}
				next.ExpectedStatementRevision = committed.StatementRevision
				next.Comments = map[string]string{"PROJECTION-1": "second text save"}
				second, err := save(next)
				if err != nil {
					t.Fatal(err)
				}
				if len(second.AffectedStatements) != 1 {
					t.Fatalf("text save republished unrelated statements: %+v", second.AffectedStatements)
				}
				for _, pageKind := range []string{"driver", "investor"} {
					got, err := repo.ReadPreparedPayrollPage(ctx, pageKind, week, PayPageQuery{})
					if err != nil || got.State != "ready" {
						t.Fatal("text publication freshness", err)
					}
					tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
					if err != nil {
						t.Fatal(err)
					}
					reports, err := readPayrollProjectionReference(ctx, tx, week, true)
					_ = tx.Rollback(ctx)
					if err != nil {
						t.Fatal(err)
					}
					i := 0
					if pageKind == "investor" {
						i = 1
					}
					want := PaginatePay(reports[i], PayPageQuery{})
					a, _ := json.Marshal(want)
					b, _ := json.Marshal(got.Report)
					if string(a) != string(b) {
						t.Fatalf("text save authoritative mismatch %s\nwant %s\ngot %s", pageKind, a, b)
					}
				}
			})
		}
		// Publication failure must roll back the canonical edit and receipt too.
		page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil {
			t.Fatal(err)
		}
		edits := page.Report.Drivers[0].Edits
		edits.ExpectedStatementRevision = page.Statements[edits.DriverID].Revision
		edits.RequestID = "18200000-0000-0000-0000-000000000013"
		edits.Notes = "must roll back"
		if _, err = admin.Exec(ctx, `CREATE FUNCTION reject_command_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test publication failure'; END $$;
CREATE TRIGGER reject_command_projection BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION reject_command_projection()`); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Save(ctx, edits, actor); err == nil {
			t.Fatal("publication failure ignored")
		}
		var notes string
		var receipts int
		if err = pool.QueryRow(ctx, `SELECT notes FROM driver_pay_weeks WHERE driver_id=$1::uuid AND week_start=$2`, projectionDriver, week).Scan(&notes); err != nil || notes != "durable comment" {
			t.Fatal("canonical edit escaped rollback", notes, err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_command_receipts WHERE request_id=$1::uuid`, edits.RequestID).Scan(&receipts); err != nil || receipts != 0 {
			t.Fatal("failed command receipt retained", receipts, err)
		}
	})
}
