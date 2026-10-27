package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
)

func testTollFlagReviews(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(sql string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	exec(`INSERT INTO trucks(unit_number) VALUES('REVIEW-TRUCK'),('REVIEW-NOHISTORY')`)
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,truck_unit,pickup_time,delivery_time) VALUES(992001,'REVIEW-PAST','delivered',100,100,'REVIEW-TRUCK','2026-09-01','2026-09-02')`)
	repo := NewTollRepository(pool)
	makeToll := func(n int64, unit string) prepass.Transaction {
		return prepass.Transaction{TollID: n, AccountNumber: json.Number("1"), PostDateTime: "2026-10-05T12:00:00", ExitDateTime: "2026-10-03T12:00:00", VehicleNumber: unit, PPDeviceID: "REVIEW-TAG", DeviceNumber: "device-value", TollCharge: json.Number("2.50")}
	}
	txns := []prepass.Transaction{makeToll(92001, "REVIEW-TRUCK"), makeToll(92002, "REVIEW-NOHISTORY"), makeToll(92003, "REVIEW-UNLINKED")}
	if _, err := repo.UpsertDay(ctx, "production", time.Now(), txns, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	tollID := scalar(`SELECT id FROM tolls WHERE prepass_toll_id=92001`)
	checkID := scalar(`SELECT id FROM tolls WHERE prepass_toll_id=92002`)
	unlinkedID := scalar(`SELECT id FROM tolls WHERE prepass_toll_id=92003`)
	find := func(id string, showReviewed bool) *Toll {
		t.Helper()
		values, err := repo.attentionTolls(ctx, "2026-09-01", showReviewed)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if v.ID == id {
				return &v
			}
		}
		return nil
	}
	value := find(tollID, false)
	if value == nil || value.Flag == nil || value.Flag.Status != "review" || len(value.ReviewRevision) != 64 {
		t.Fatalf("missing true flag: %+v", value)
	}
	if value.Flag.TransactionDate != "2026-10-03" {
		t.Fatal("flag used posting date instead of exit date")
	}
	if find(checkID, false) != nil {
		t.Fatal("linked Check data row remained in attention")
	}
	if v := find(unlinkedID, false); v == nil || v.Flag != nil {
		t.Fatal("unlinked rows should remain without Check data hints")
	}
	before := scalar(`SELECT to_jsonb(t)::text FROM tolls t WHERE id=$1`, tollID)
	in := TollFlagReviewInput{ID: scalar(`SELECT gen_random_uuid()`), Revision: value.ReviewRevision, Reviewed: true}
	if err := repo.ReviewTollFlag(ctx, tollID, in, actor); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewTollFlag(ctx, tollID, in, actor); err != nil {
		t.Fatal("idempotent review", err)
	}
	if find(tollID, false) != nil {
		t.Fatal("reviewed flag remained visible")
	}
	checked := find(tollID, true)
	if checked == nil || !checked.Reviewed || checked.ReviewedAt == "" || checked.ReviewedBy == "" || checked.ReviewVersion != in.ID {
		t.Fatalf("missing review attribution: %+v", checked)
	}
	if scalar(`SELECT to_jsonb(t)::text FROM tolls t WHERE id=$1`, tollID) != before {
		t.Fatal("review changed source or financial data")
	}
	if _, err := repo.UpsertDay(ctx, "production", time.Now(), txns, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE loads SET synced_at=now() WHERE id=992001`)
	if find(tollID, false) != nil {
		t.Fatal("routine resync reopened review")
	}
	reopen := TollFlagReviewInput{ID: scalar(`SELECT gen_random_uuid()`), Revision: checked.ReviewRevision, Version: checked.ReviewVersion, Reviewed: false}
	if err := repo.ReviewTollFlag(ctx, tollID, reopen, actor); err != nil {
		t.Fatal(err)
	}
	if v := find(tollID, false); v == nil || v.Reviewed {
		t.Fatal("reopen did not restore flag")
	}
	if err := repo.ReviewTollFlag(ctx, tollID, in, actor); err != nil {
		t.Fatal("old receipt replay", err)
	}
	if find(tollID, false) == nil {
		t.Fatal("old receipt reapplied after reopen")
	}
	stale := in
	stale.ID = scalar(`SELECT gen_random_uuid()`)
	if repo.ReviewTollFlag(ctx, tollID, stale, actor) == nil {
		t.Fatal("stale review version accepted")
	}
	value = find(tollID, false)
	in = TollFlagReviewInput{ID: scalar(`SELECT gen_random_uuid()`), Revision: value.ReviewRevision, Version: value.ReviewVersion, Reviewed: true}
	if err := repo.ReviewTollFlag(ctx, tollID, in, actor); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE tolls SET amount=9 WHERE id=$1`, tollID)
	changed := find(tollID, false)
	if changed == nil || changed.Reviewed || changed.ReviewRevision == in.Revision {
		t.Fatal("changed source did not require new review")
	}
	stale = in
	stale.ID = scalar(`SELECT gen_random_uuid()`)
	stale.Version = changed.ReviewVersion
	if repo.ReviewTollFlag(ctx, tollID, stale, actor) == nil {
		t.Fatal("stale source revision accepted")
	}
	in = TollFlagReviewInput{ID: scalar(`SELECT gen_random_uuid()`), Revision: changed.ReviewRevision, Version: changed.ReviewVersion, Reviewed: true}
	if err := repo.ReviewTollFlag(ctx, tollID, in, actor); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE loads SET delivery_time='2026-09-03' WHERE id=992001`)
	if find(tollID, false) == nil {
		t.Fatal("changed load evidence did not reopen review")
	}
	if repo.ReviewTollFlag(ctx, checkID, TollFlagReviewInput{ID: scalar(`SELECT gen_random_uuid()`), Revision: in.Revision, Reviewed: true}, actor) == nil {
		t.Fatal("accepted data issue as spending flag")
	}
	if scalar(`SELECT count(*) FROM toll_flag_review_events WHERE toll_id=$1`, tollID) != "4" {
		t.Fatal("missing immutable review/reopen receipts")
	}
}
