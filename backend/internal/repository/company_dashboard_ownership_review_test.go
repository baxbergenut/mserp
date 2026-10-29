package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDashboardLoadOwnershipReviewDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(sql string) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES
 ('10000000-0000-0000-0000-000000000001','Review owner','review owner','gross_percentage',87),
 ('10000000-0000-0000-0000-000000000002','Review hired','review hired','cpm',0.6);
 INSERT INTO investors(id,full_name,driver_id) VALUES
 ('20000000-0000-0000-0000-000000000001','Review owner','10000000-0000-0000-0000-000000000001'),
 ('20000000-0000-0000-0000-000000000002','Review investor',null);
 INSERT INTO trucks(id,unit_number,owner_id) VALUES
 ('30000000-0000-0000-0000-000000000001','REVIEW-OWNER','20000000-0000-0000-0000-000000000001'),
 ('30000000-0000-0000-0000-000000000002','REVIEW-INVESTOR','20000000-0000-0000-0000-000000000002');
 UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true
 WHERE truck_unit IN ('REVIEW-OWNER','REVIEW-INVESTOR');
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES
 ('30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','2026-10-20 00:00-04'),
 ('30000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000002','2026-09-01');
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,truck_id,raw_payload) VALUES
 (99001,'UNLINKED-SOURCE','delivered',1000,1000,500,'REVIEW-UNLINKED',null,'{}'),
 (99002,'MISSING-TERMS','delivered',2000,2000,600,'REVIEW-INVESTOR','30000000-0000-0000-0000-000000000002','{}'),
 (99003,'OWNER-DRIVING','delivered',3000,3000,700,'REVIEW-OWNER','30000000-0000-0000-0000-000000000001','{}'),
 (99004,'STALE-PROVIDER-TRUCK','delivered',1000,1000,500,'01 PACIFIQUE',null,'{}');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES
 ('10000000-0000-0000-0000-000000000001','2026-10-19','UNLINKED-SOURCE',99001,900),
 ('10000000-0000-0000-0000-000000000002','2026-10-20','MISSING-TERMS',99002,1800),
 ('10000000-0000-0000-0000-000000000001','2026-10-21','OWNER-DRIVING',99003,2800),
 ('10000000-0000-0000-0000-000000000001','2026-10-22','STALE-PROVIDER-TRUCK',99004,900);`)
		p, _ := ResolveDashboardPeriod("week", "2026-10-19", "", time.Now())
		repo := NewDashboardRepository(pool)
		page, err := repo.DashboardCostReview(ctx, p, "ownership", Pagination{Page: 1, PageSize: 1})
		if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Reference != "UNLINKED-SOURCE" {
			t.Fatalf("ownership review page: %+v %v", page, err)
		}
		item := page.Items[0]
		if item.TruckID != "" || item.TruckUnit != "Unknown truck" || item.DriverName != "Review owner" || item.Amount == nil || *item.Amount != "1000.00" || len(item.AssignedUnits) != 0 || item.Reason != "Clustie truck assignment is missing or conflicting on the load date" {
			t.Fatalf("missing native assignment must remain in review: %+v", item)
		}
		page, err = repo.DashboardCostReview(ctx, p, "ownership", Pagination{Page: 2, PageSize: 1})
		if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Reference != "MISSING-TERMS" || page.Items[0].Reason != "Investor settlement terms are missing or name a different owner" {
			t.Fatalf("terms review page: %+v %v", page, err)
		}
		flow, err := repo.Waterfall(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, review := range flow.Review {
			if review.ID == "ownership" {
				found = true
				if review.Count != page.Total {
					t.Fatalf("count and drilldown differ: %+v / %+v", review, page)
				}
			}
		}
		if !found || flow.Profit != nil {
			t.Fatalf("unresolved ownership must retain the profit review: %+v", flow)
		}
		// Correcting the native assignment and configuring the agreement removes
		// both rows, even while DataTruck's truck labels remain incorrect.
		exec(`UPDATE truck_driver_assignments SET assigned_at='2026-09-01' WHERE driver_id='10000000-0000-0000-0000-000000000001';
 INSERT INTO truck_settlement_terms(truck_id,week_start,owner_id,share_percent) VALUES
 ('30000000-0000-0000-0000-000000000002','2026-10-19','20000000-0000-0000-0000-000000000002',88);`)
		page, err = repo.DashboardCostReview(ctx, p, "ownership", Pagination{})
		if err != nil || page.Total != 0 || len(page.Items) != 0 {
			t.Fatalf("resolved records remain in ownership review: %+v %v", page, err)
		}
	})
}
