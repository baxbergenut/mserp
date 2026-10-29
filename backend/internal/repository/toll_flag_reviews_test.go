package repository

import (
	"context"
	"encoding/json"
	"sort"
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
	driver := scalar(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Flag Review Driver','flag review driver','cpm',0.7) RETURNING id`)
	exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) SELECT id,$1,'2026-01-01' FROM trucks WHERE unit_number='REVIEW-TRUCK'`, driver)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-09-01','REVIEW-PAST',992001)`, driver)
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
		for pageIndex := 1; pageIndex <= max(1, len(values)); pageIndex++ {
			page, e := repo.AttentionTolls(ctx, "2026-09-01", showReviewed, Pagination{Page: pageIndex, PageSize: 1})
			if e != nil || page.Total != len(values) {
				t.Fatal("attention pagination count", page, e)
			}
			if len(values) > 0 {
				a, _ := json.Marshal(values[pageIndex-1])
				b, _ := json.Marshal(page.Items[0])
				if string(a) != string(b) {
					t.Fatal("attention pagination changed evidence", string(a), string(b))
				}
			}
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
	// Bulk review is atomic, scoped to explicit IDs, and keeps individual receipts.
	bulkTxns := []prepass.Transaction{makeToll(92010, "REVIEW-TRUCK"), makeToll(92011, "REVIEW-TRUCK"), makeToll(92012, "REVIEW-TRUCK")}
	if _, err := repo.UpsertDay(ctx, "production", time.Now(), bulkTxns, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	bulkIDs := []string{scalar(`SELECT id FROM tolls WHERE prepass_toll_id=92010`), scalar(`SELECT id FROM tolls WHERE prepass_toll_id=92011`)}
	changes := []TollFlagReviewChange{}
	for _, id := range bulkIDs {
		v := find(id, false)
		changes = append(changes, TollFlagReviewChange{TollID: id, ID: scalar(`SELECT gen_random_uuid()`), Revision: v.ReviewRevision, Version: v.ReviewVersion})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].TollID < changes[j].TollID })
	exec(`UPDATE tolls SET amount=5 WHERE id=$1`, changes[1].TollID)
	if repo.ReviewTollFlagsBulk(ctx, changes, actor) == nil {
		t.Fatal("bulk accepted stale evidence")
	}
	if scalar(`SELECT count(*) FROM toll_flag_review_events WHERE toll_id=ANY($1::uuid[])`, bulkIDs) != "0" || find(changes[0].TollID, false) == nil {
		t.Fatal("bulk failure partially reviewed selection")
	}
	changes[1].Revision = find(changes[1].TollID, false).ReviewRevision
	beforeBulk := scalar(`SELECT jsonb_agg(to_jsonb(t) ORDER BY t.id)::text FROM tolls t WHERE id=ANY($1::uuid[])`, bulkIDs)
	if err := repo.ReviewTollFlagsBulk(ctx, changes, actor); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewTollFlagsBulk(ctx, changes, actor); err != nil {
		t.Fatal("bulk retry", err)
	}
	if scalar(`SELECT count(*) FROM toll_flag_review_events WHERE toll_id=ANY($1::uuid[])`, bulkIDs) != "2" {
		t.Fatal("bulk did not retain exactly one receipt per transaction")
	}
	for _, id := range bulkIDs {
		if find(id, false) != nil || !find(id, true).Reviewed {
			t.Fatal("bulk flag remained in attention")
		}
	}
	if find(scalar(`SELECT id FROM tolls WHERE prepass_toll_id=92012`), false) == nil {
		t.Fatal("bulk reviewed an unselected flag")
	}
	if scalar(`SELECT jsonb_agg(to_jsonb(t) ORDER BY t.id)::text FROM tolls t WHERE id=ANY($1::uuid[])`, bulkIDs) != beforeBulk {
		t.Fatal("bulk modified source or financial records")
	}
	checked = find(changes[0].TollID, true)
	if err := repo.ReviewTollFlag(ctx, checked.ID, TollFlagReviewInput{ID: scalar(`SELECT gen_random_uuid()`), Revision: checked.ReviewRevision, Version: checked.ReviewVersion, Reviewed: false}, actor); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewTollFlagsBulk(ctx, changes, actor); err != nil {
		t.Fatal("bulk receipt replay after reopen", err)
	}
	if find(checked.ID, false) == nil {
		t.Fatal("bulk replay reapplied a reopened review")
	}
	already := find(changes[1].TollID, true)
	invalidCases := [][]TollFlagReviewChange{
		{}, make([]TollFlagReviewChange, 101), {changes[0], changes[0]},
		{{TollID: changes[0].TollID, ID: changes[0].ID}, {TollID: changes[1].TollID, ID: changes[0].ID}},
		{{TollID: unlinkedID, ID: scalar(`SELECT gen_random_uuid()`), Revision: in.Revision}},
		{{TollID: checkID, ID: scalar(`SELECT gen_random_uuid()`), Revision: in.Revision}},
		{{TollID: already.ID, ID: scalar(`SELECT gen_random_uuid()`), Revision: already.ReviewRevision, Version: already.ReviewVersion}},
	}
	for i, selection := range invalidCases {
		if repo.ReviewTollFlagsBulk(ctx, selection, actor) == nil {
			t.Fatalf("accepted invalid bulk selection %d", i)
		}
	}
}
