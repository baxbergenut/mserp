package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCompanyDashboardPeriods(t *testing.T) {
	for _, tc := range []struct{ preset, now, from, to, previousFrom, previousTo string }{
		{"thisWeek", "2026-10-27T02:00:00Z", "2026-10-26", "2026-10-26", "2026-10-19", "2026-10-19"},
		{"lastWeek", "2026-11-02T17:00:00Z", "2026-10-26", "2026-11-01", "2026-10-19", "2026-10-25"},
		{"thisMonth", "2026-03-31T12:00:00Z", "2026-03-01", "2026-03-31", "2026-02-01", "2026-02-28"},
		{"lastMonth", "2026-03-15T12:00:00Z", "2026-02-01", "2026-02-28", "2026-01-01", "2026-01-31"},
		{"thisWeek", "2026-01-01T12:00:00Z", "2025-12-29", "2026-01-01", "2025-12-22", "2025-12-25"},
	} {
		t.Run(tc.now+tc.preset, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.now)
			p, err := ResolveDashboardPeriod(tc.preset, "", "", now)
			if err != nil || p.DateFrom != tc.from || p.DateTo != tc.to || p.CompareFrom != tc.previousFrom || p.CompareTo != tc.previousTo {
				t.Fatalf("period=%+v err=%v", p, err)
			}
		})
	}
	for _, dates := range [][2]string{{"2026-02-30", "2026-03-01"}, {"2026-02-10", "2026-02-01"}, {"2025-01-01", "2026-01-02"}, {"1999-01-01", "1999-01-02"}} {
		if _, err := ResolveDashboardPeriod("custom", dates[0], dates[1], time.Now()); err == nil {
			t.Fatalf("accepted invalid dates %v", dates)
		}
	}
	p, err := ResolveDashboardPeriod("custom", "2024-01-01", "2024-12-31", time.Now())
	if err != nil || p.Bucket != "week" {
		t.Fatalf("leap year: %+v %v", p, err)
	}
}

func TestCompanyDashboardDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DASHBOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DASHBOARD_TEST_DATABASE_URL to a disposable local _test database")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_test") || (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost") {
		t.Fatal("use a disposable local _test database")
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE drivers(id uuid PRIMARY KEY,full_name text);
 CREATE TEMP TABLE dispatchers(id uuid PRIMARY KEY,full_name text);
 CREATE TEMP TABLE driver_dispatcher_assignments(driver_id uuid,dispatcher_id uuid,dispatcher_name text,assigned_at timestamptz,unassigned_at timestamptz,start_known boolean);
 CREATE TEMP TABLE loads(id int PRIMARY KEY,load_id text,total_pay numeric(12,2),total_miles numeric(12,2));
 CREATE TEMP TABLE gross_board_entries(driver_id uuid,service_date date,load_number text,load_record_id int,original_rate numeric(12,2),driver_rate numeric(12,2),miles numeric(12,2),version int,entered_original_rate numeric(12,2),entered_miles numeric(12,2),day_status text DEFAULT '',note text);
 CREATE TEMP TABLE gross_board_extra_entries(LIKE gross_board_entries);ALTER TABLE gross_board_extra_entries ADD COLUMN slot int;ALTER TABLE gross_board_extra_entries ADD COLUMN deleted boolean DEFAULT false;
 INSERT INTO drivers VALUES('00000000-0000-0000-0000-000000000001','Driver A'),('00000000-0000-0000-0000-000000000002','Driver B');
 INSERT INTO dispatchers VALUES('00000000-0000-0000-0000-000000000010','Dispatch A'),('00000000-0000-0000-0000-000000000011','Dispatch B');
 INSERT INTO driver_dispatcher_assignments VALUES('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000010','Dispatch A','2026-01-01 00:00-05','2026-10-21 00:00-04',true),('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000011','Dispatch B','2026-10-21 00:00-04',null,true);
 INSERT INTO loads VALUES(1,'REPEAT',1000,500),(2,'OVERRIDE',900,450),(3,'NO-MILES',200,null),(4,'CONFLICT',500,100),(5,'AMBIGUOUS',500,100),(6,'AMBIGUOUS',600,100),(7,'NO-RATE',null,300);
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate,entered_original_rate,entered_miles) VALUES
 ('00000000-0000-0000-0000-000000000001','2026-10-12','REPEAT',1,800,null,null),
 ('00000000-0000-0000-0000-000000000001','2026-10-19','REPEAT',1,800,null,null),
 ('00000000-0000-0000-0000-000000000001','2026-10-20','OVERRIDE',2,1000,1200,400),
 ('00000000-0000-0000-0000-000000000001','2026-10-21','NO-MILES',3,150,null,null),
 ('00000000-0000-0000-0000-000000000001','2026-10-22','CONFLICT',4,400,null,null),
 ('00000000-0000-0000-0000-000000000002','2026-10-22','CONFLICT',4,400,null,null),
 ('00000000-0000-0000-0000-000000000001','2026-10-23','AMBIGUOUS',null,250,300,100),
 ('00000000-0000-0000-0000-000000000001','2026-10-24','NO-RATE',7,null,null,null);
 INSERT INTO gross_board_extra_entries(driver_id,service_date,load_number,load_record_id,driver_rate,slot,deleted,day_status) VALUES
 ('00000000-0000-0000-0000-000000000001','2026-10-20','OVERRIDE',2,1000,1,true,''),
 ('00000000-0000-0000-0000-000000000001','2026-10-25','NO-MILES',3,150,1,false,'SHOP');
 UPDATE gross_board_entries SET original_rate=999 WHERE load_record_id=7;
 UPDATE gross_board_entries SET miles=999 WHERE load_record_id=3;`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewDashboardRepository(pool)
	p, _ := ResolveDashboardPeriod("custom", "2026-10-19", "2026-10-25", time.Now())
	v, err := repo.Performance(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if v.Current.Gross != "1400.00" || v.Current.DriverGross != "1150.00" || v.Current.Loads != 3 || v.Current.Miles != "700.00" || v.Current.RPM == nil || *v.Current.RPM != "3.00" || v.Current.PairedLoads != 1 || v.Current.MissingGross != 1 || v.Current.MissingMiles != 1 || v.Current.MissingDriverGross != 1 || v.Current.Conflicts != 2 || v.Current.Repeats != 1 || v.Current.Provisional != 1 || v.Current.RawGross != "3700.00" {
		t.Fatalf("bad totals %+v", v.Current)
	}
	if v.Previous.Gross != "1000.00" || len(v.Dispatchers) != 3 {
		t.Fatalf("bad comparison/breakdown %+v", v)
	}
	page, err := repo.DashboardLoads(ctx, p, "", "", Pagination{Page: 99, PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 || page.Page != 3 || len(page.Items) != 1 {
		t.Fatalf("bad pagination %+v", page)
	}
	page, err = repo.DashboardLoads(ctx, p, "unassigned", "conflict", Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].DriverName != "Driver B" {
		t.Fatalf("bad unassigned %+v", page)
	}
	page, err = repo.DashboardLoads(ctx, p, "", "repeat", Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].LoadNumber != "REPEAT" {
		t.Fatalf("bad repeats %+v", page)
	}
	if _, err = pool.Exec(ctx, `UPDATE gross_board_entries SET driver_rate=801 WHERE load_record_id=1 AND service_date='2026-10-19'`); err != nil {
		t.Fatal(err)
	}
	v, err = repo.Performance(ctx, p)
	if err != nil || v.Current.Conflicts != 3 || v.Current.Repeats != 0 || v.Previous.Loads != 0 {
		t.Fatalf("cross-period value conflict %+v: %v", v, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE gross_board_entries SET driver_rate=800 WHERE load_record_id=1`); err != nil {
		t.Fatal(err)
	}
	empty, _ := ResolveDashboardPeriod("custom", "2028-01-01", "2028-01-07", time.Now())
	v, err = repo.Performance(ctx, empty)
	if err != nil || v.Current.Loads != 0 || v.Current.RPM != nil {
		t.Fatalf("empty=%+v err=%v", v, err)
	}
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE expense_settings(id uuid,name text);CREATE TEMP TABLE expenses(category_id uuid,expense_date date,amount numeric(12,2));
 INSERT INTO expense_settings VALUES('00000000-0000-0000-0000-000000000020','Visible'),('00000000-0000-0000-0000-000000000021','Hidden');
 INSERT INTO expenses VALUES('00000000-0000-0000-0000-000000000020','2026-10-19',12.34),('00000000-0000-0000-0000-000000000020','2026-10-20',null),('00000000-0000-0000-0000-000000000020',null,5),('00000000-0000-0000-0000-000000000021','2026-10-19',9999);
 CREATE TEMP TABLE tolls(posting_date date,amount numeric(12,2),prepass_environment text);INSERT INTO tolls VALUES('2026-10-19',10.10,'production'),('2026-10-20',-1.01,null),('2026-10-20',999,'sandbox');
 CREATE TEMP TABLE weighmytruck_transactions(transaction_date date,amount numeric(12,2));INSERT INTO weighmytruck_transactions VALUES('2026-10-19',15.00);
 CREATE TEMP TABLE fuel_transactions(id int,purchased_on date,state text);CREATE TEMP TABLE fuel_transaction_items(fuel_transaction_id int,total_amount_paid numeric,total_retail_price numeric,quantity numeric,unit_of_measure text,item_kind text,category text);
 INSERT INTO fuel_transactions VALUES(1,'2026-10-19','OH');INSERT INTO fuel_transaction_items VALUES(1,100,110,30,'gallons','fuel','diesel'),(1,50,50,10,'gallons','fuel','def'),(1,20,20,1,'each','product','other');`)
	if err != nil {
		t.Fatal(err)
	}
	for kind, want := range map[string]string{"expenses": "12.34", "tolls": "9.09", "scale": "15.00", "diesel": "100"} {
		items, err := repo.DashboardCost(ctx, kind, p, []string{"00000000-0000-0000-0000-000000000020"})
		if err != nil {
			t.Fatal(kind, err)
		}
		if len(items) != 1 || items[0].Amount == nil || *items[0].Amount != want {
			t.Fatalf("%s costs=%+v", kind, items)
		}
		if kind == "expenses" && (items[0].Label != "Visible" || items[0].Incomplete != 2) {
			t.Fatalf("category boundary %+v", items)
		}
	}
	items, err := repo.DashboardCost(ctx, "expenses", p, []string{})
	if err != nil || len(items) != 0 {
		t.Fatalf("empty category permission leak %+v %v", items, err)
	}
	// Exercise a year of reporting with a representative multi-thousand-load set.
	_, err = pool.Exec(ctx, `CREATE INDEX ON loads(lower(btrim(load_id)));
 INSERT INTO loads SELECT n+100,'VOLUME-'||n,1000,500 FROM generate_series(1,5000) n;
 INSERT INTO gross_board_extra_entries(driver_id,service_date,load_number,load_record_id,driver_rate,slot,day_status)
 SELECT '00000000-0000-0000-0000-000000000001'::uuid,'2026-01-01'::date+(n-1)%280,'VOLUME-'||n,n+100,800,n,'' FROM generate_series(1,5000) n;
 ANALYZE loads; ANALYZE gross_board_extra_entries;`)
	if err != nil {
		t.Fatal(err)
	}
	year, _ := ResolveDashboardPeriod("custom", "2026-01-01", "2026-12-31", time.Now())
	started := time.Now()
	v, err = repo.Performance(ctx, year)
	if err != nil || v.Current.Loads != 5004 || v.Current.Gross != "5002400.00" {
		t.Fatalf("volume totals %+v: %v", v.Current, err)
	}
	t.Logf("5000-load annual report: %s", time.Since(started))
}

// Uses the real initialized schema, unlike the focused aggregation fixtures above.
func TestCompanyDashboardSchema(t *testing.T) {
	dsn := os.Getenv("MSERP_DASHBOARD_SCHEMA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DASHBOARD_SCHEMA_TEST_DATABASE_URL to an initialized disposable local _test database")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_test") || (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost") {
		t.Fatal("use a disposable local _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewDashboardRepository(pool)
	p, _ := ResolveDashboardPeriod("thisMonth", "", "", time.Now())
	for _, kind := range []string{"trucks", "trailers", "drivers", "gps"} {
		if _, err := repo.DashboardCounts(ctx, kind); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	for _, kind := range []string{"diesel", "tolls", "scale", "expenses"} {
		if _, err := repo.DashboardCost(ctx, kind, p, []string{}); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if _, err := repo.Performance(ctx, p); err != nil {
		t.Error(err)
	}
	if _, err := repo.DashboardLoads(ctx, p, "", "", Pagination{Page: 1, PageSize: 25}); err != nil {
		t.Error(err)
	}
	for _, admin := range []bool{true, false} {
		if _, err := NewCustomTaskRepository(pool).DashboardTasks(WithTaskViewer(ctx, "00000000-0000-0000-0000-000000000001", admin), true, true); err != nil {
			t.Error(err)
		}
	}
	if _, err := NewMaintenanceRepository(pool).ProgramTrucks(ctx, "", ""); err != nil {
		t.Error(err)
	}
	if _, err := NewTollRepository(pool).ListTollsPage(ctx, TollPageQuery{Unlinked: true, Pagination: Pagination{Page: 1, PageSize: 25}}); err != nil {
		t.Error(err)
	}
}
