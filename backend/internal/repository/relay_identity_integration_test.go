package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/relay"
)

// Tables, functions and triggers live in a disposable schema. Repeat runs never
// leave functions in public or mutate another test fixture.
func TestRelayIdentityDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_RELAY_REVIEW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_RELAY_REVIEW_TEST_DATABASE_URL for temporary-table PostgreSQL checks")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("disposable test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer pool.Close()
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	testSchema := fmt.Sprintf("relay_identity_%d", time.Now().UnixNano())
	if _, err = pool.Exec(ctx, `CREATE SCHEMA `+testSchema+`;SET search_path TO `+testSchema+`,public`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `SET search_path TO public;DROP SCHEMA `+testSchema+` CASCADE`)
	}()
	schema := string(source)
	if _, err = pool.Exec(ctx, schema); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous constraints on empty temporary tables and exercise
	// the actual upgrade migration, as well as the fresh-install schema above.
	_, err = pool.Exec(ctx, `DROP TABLE relay_identity_reviews;
	DROP INDEX relay_driver_links_pending_idx;
	DROP INDEX fuel_transactions_relay_identity_idx;
	ALTER TABLE relay_driver_links ALTER COLUMN driver_id SET NOT NULL;
	ALTER TABLE fuel_transactions ALTER COLUMN driver_id SET NOT NULL;`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/024_add_relay_identity_review.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	const driverA = "00000000-0000-0000-0000-000000000001"
	const driverB = "00000000-0000-0000-0000-000000000002"
	const user = "00000000-0000-0000-0000-000000000003"
	_, err = pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,is_owner_operator,pay_type,pay_rate,phone,email)
 VALUES('00000000-0000-0000-0000-000000000001','James Lee Burligh','james lee burligh',true,'gross_percentage',90,'+14703344443','james@example.com'),
 ('00000000-0000-0000-0000-000000000002','Other Person','other person',false,'cpm',0,'+14703344443','shared@example.com');
 INSERT INTO app_users(id,username,password_hash) VALUES('00000000-0000-0000-0000-000000000003','reviewer','$2test');
 INSERT INTO trucks(unit_number) VALUES('001');`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewFuelRepository(pool)
	when := time.Date(2026, 9, 21, 15, 0, 0, 0, time.UTC)
	until := when.AddDate(0, 0, 1)
	makePurchase := func(id, account string) relay.Transaction {
		var purchase relay.Transaction
		// Decode through the upstream JSON types to exercise their real decimal parser.
		return relayTestPurchase(t, id, account, when, purchase)
	}
	first := makePurchase("purchase-1", "new-account")
	if err = repo.UpsertDay(ctx, "production", when, []relay.Transaction{first}, when, true); err != nil {
		t.Fatal(err)
	}
	tasks, err := repo.RelayIdentityTasks(ctx, Pagination{Page: 1, PageSize: 25}, "")
	if err != nil || tasks.Total != 1 || tasks.Items[0].TransactionCount != 1 || len(tasks.Items[0].Suggestions) != 2 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	id := tasks.Items[0].ID
	transactions, err := repo.ListTransactionsPage(ctx, FuelPageQuery{Pagination: Pagination{Page: 1, PageSize: 25}})
	if err != nil {
		t.Fatal(err)
	}
	if transactions.Total != 1 || transactions.Items[0].DriverID != nil || transactions.Summary.Spend != 100 || transactions.Items[0].Flag.Status != "data_issue" {
		t.Fatalf("unassigned purchase lost: %+v", transactions)
	}
	completed, err := repo.CompletedDays(ctx, "production", when, when)
	if _, ok := completed[when.Format(time.DateOnly)]; err != nil || !ok {
		t.Fatalf("day not completed: %v", err)
	}
	dashboard := FinancialDashboard{}
	dashboardRepo := NewDashboardRepository(pool)
	if err = dashboardRepo.loadFinancialTotals(ctx, FinancialDashboardQuery{DateFrom: &when, DateTo: &until}, &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.Totals.Fuel != 100 || dashboard.Totals.UnattributedFuel != 100 {
		t.Fatalf("unassigned financial totals=%+v", dashboard.Totals)
	}
	if _, err = repo.ReviewRelayIdentity(ctx, id, driverB, "reject", user); err != nil {
		t.Fatal(err)
	}
	tasks, err = repo.RelayIdentityTasks(ctx, Pagination{Page: 1, PageSize: 25}, "")
	if err != nil || len(tasks.Items[0].Suggestions) != 1 || tasks.Items[0].Suggestions[0].DriverID != driverA {
		t.Fatalf("rejection lost: %+v %v", tasks, err)
	}
	// Audit failure must roll back the mapping and backfill.
	if _, err = repo.ReviewRelayIdentity(ctx, id, driverA, "link", "00000000-0000-0000-0000-000000000099"); err == nil {
		t.Fatal("expected invalid reviewer rollback")
	}
	if _, err = repo.ReviewRelayIdentity(ctx, id, driverA, "link", user); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ReviewRelayIdentity(ctx, id, driverA, "link", user); err != nil || n != 0 {
		t.Fatalf("retry=%d %v", n, err)
	}
	if _, err = repo.ReviewRelayIdentity(ctx, id, driverB, "link", user); !errors.Is(err, ErrRelayReviewConflict) {
		t.Fatalf("conflict=%v", err)
	}
	first.Driver.FirstName = "Changed"
	if err = repo.UpsertDay(ctx, "production", when, []relay.Transaction{first, makePurchase("purchase-2", "new-account")}, when, true); err != nil {
		t.Fatal(err)
	}
	// Changed profile and reimport cannot overwrite the confirmed mapping.
	rows, err := repo.ListTransactions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("duplicate/lost purchases=%d", len(rows))
	}
	for _, row := range rows {
		if row.DriverID == nil || *row.DriverID != driverA {
			t.Fatal("confirmed link lost")
		}
	}
	dashboard = FinancialDashboard{}
	if err = dashboardRepo.loadFinancialTotals(ctx, FinancialDashboardQuery{DateFrom: &when, DateTo: &until}, &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.Totals.UnattributedFuel != 0 || dashboard.Totals.DeductedFuel != 200 || dashboard.Totals.Fuel != 0 {
		t.Fatalf("linked financial totals=%+v", dashboard.Totals)
	}
	// A changed Relay ID and a staging identity with the same ID both need review.
	if err = repo.UpsertDay(ctx, "production", when, []relay.Transaction{makePurchase("purchase-3", "second-account")}, when, true); err != nil {
		t.Fatal(err)
	}
	if err = repo.UpsertDay(ctx, "staging", when, []relay.Transaction{makePurchase("purchase-1", "new-account")}, when, true); err != nil {
		t.Fatal(err)
	}
	tasks, err = repo.RelayIdentityTasks(ctx, Pagination{Page: 1, PageSize: 25}, "")
	if err != nil || tasks.Total != 2 {
		t.Fatalf("environment/new ID tasks=%+v %v", tasks, err)
	}
	for _, task := range tasks.Items {
		if task.Environment == "production" {
			if n, err := repo.ReviewRelayIdentity(ctx, task.ID, driverA, "link", user); err != nil || n != 1 {
				t.Fatalf("many IDs to one driver: %d %v", n, err)
			}
		}
	}
	// No source creates fleet masters, including unknown primary/team drivers.
	loadRepo := NewLoadRepository(pool)
	unknown := "Unknown Person"
	team := "New Team Person"
	unit := "9999"
	records := []LoadRecord{{ID: 1, LoadID: "L-1", Status: "delivered", DriverName: &unknown, TeamDriverName: &team, TruckUnit: &unit, LoadPay: "0", TotalOtherPay: "0", TotalPay: "0", SyncedAt: when, RawPayload: []byte("{}")}}
	if err = loadRepo.UpsertLoads(ctx, records); err != nil {
		t.Fatal(err)
	}
	var drivers, trucks, assignments int
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM drivers),(SELECT count(*) FROM trucks),(SELECT count(*) FROM truck_driver_assignments)").Scan(&drivers, &trucks, &assignments); err != nil {
		t.Fatal(err)
	}
	if drivers != 2 || trucks != 1 || assignments != 0 {
		t.Fatalf("unexpected fleet creation: %d %d %d", drivers, trucks, assignments)
	}
	var loadedID *string
	var sourceName, sourceUnit string
	if err = pool.QueryRow(ctx, "SELECT driver_id,driver_name,truck_unit FROM loads WHERE id=1").Scan(&loadedID, &sourceName, &sourceUnit); err != nil {
		t.Fatal(err)
	}
	if loadedID != nil || sourceName != unknown || sourceUnit != unit {
		t.Fatal("unmatched load snapshot lost")
	}
	// Even a matched primary/team driver must not be renamed, assigned a
	// dispatcher, or assigned to the source truck by any DataTruck path.
	name, dispatch, knownUnit := "Burligh James", "Unknown Dispatcher", "001"
	records[0].DriverName, records[0].TeamDriverName = &name, &name
	records[0].DispatcherName, records[0].TruckUnit = &dispatch, &knownUnit
	for _, importLoads := range []func(context.Context, []LoadRecord) error{loadRepo.UpsertLoads, loadRepo.RefreshLoads, loadRepo.ReconcileLoads} {
		if err = importLoads(ctx, records); err != nil {
			t.Fatal(err)
		}
		var fleetName string
		var dispatcher *string
		var count int
		if err = pool.QueryRow(ctx, `SELECT full_name,dispatcher_id,(SELECT count(*) FROM truck_driver_assignments)+(SELECT count(*) FROM dispatchers) FROM drivers WHERE id=$1`, driverA).Scan(&fleetName, &dispatcher, &count); err != nil {
			t.Fatal(err)
		}
		if fleetName != "James Lee Burligh" || dispatcher != nil || count != 0 {
			t.Fatalf("source changed managed fleet: %s %v %d", fleetName, dispatcher, count)
		}
	}

}

func relayTestPurchase(t *testing.T, id, account string, when time.Time, purchase relay.Transaction) relay.Transaction {
	t.Helper()
	payload := fmt.Sprintf(`{"transaction_id":%q,"created_at":%q,"total_amount_paid":"100.00","currency_code":"USD","driver":{"id":%q,"first_name":"Burligh","last_name":"James","phone":"+14703344443","email":"james@example.com"},"location":{"timezone":"America/New_York"},"prompts":[],"fuel_items":[{"fuel_type":"diesel","fuel_type_description":"Diesel","volume":"20","volume_uom":"gallons","total_discounted_price":"100.00","total_retail_price":"100.00"}]}`, id, when.Format(time.RFC3339), account)
	if err := json.Unmarshal([]byte(payload), &purchase); err != nil {
		t.Fatal(err)
	}
	return purchase
}
