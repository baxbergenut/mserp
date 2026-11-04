package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
	"mserp/internal/repository"
)

type modifiedTestTransport struct {
	target    *url.URL
	transport http.RoundTripper
}

func (r modifiedTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL.Scheme, copy.URL.Host = r.target.Scheme, r.target.Host
	return r.transport.RoundTrip(copy)
}

// Real HTTP decoding, keyset paging, scheduled-job checkpointing, migration and
// repository writes, ending at the actual Gross Board comparison projection.
func TestModifiedLoadsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable database required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	schema := fmt.Sprintf("modified_sync_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/196_datatruck_modified_sync.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(initial), "\r\n", "\n")
	m := strings.ReplaceAll(string(migration), "\r\n", "\n")
	if !strings.Contains(body, m) {
		t.Fatal("init schema missing exact migration")
	}
	exec(strings.Replace(body, m, "", 1))
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_other_pay,total_pay,synced_at,raw_payload) SELECT id,'MOD-'||id,'booked',2800,0,2800,now(),'{}' FROM generate_series(1,26) id`)
	exec(m)
	exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT ALL ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT ALL ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
	// Every connection, not just the migration connection, exercises app privileges.
	pool.Close()
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const driver = "81000000-0000-0000-0000-000000009601"
	exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Modified Driver','modified driver','gross_percentage',80)`, driver)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES($1,'2026-10-19','MOD-1',2300,2800,500)`, driver)
	var mu sync.Mutex
	stamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	gross, miles, total := 2000, 541, 2800
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		var filters []map[string]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filter")), &filters); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(filters) != 2 {
			t.Error("expected modified and ID filters")
			w.WriteHeader(400)
			return
		}
		since, err := time.Parse(time.RFC3339Nano, filters[0]["value"])
		if err != nil {
			t.Error(err)
			return
		}
		first, _ := strconv.Atoi(filters[1]["value"])
		rows := []map[string]any{}
		for id := max(first, 1); id <= 27 && len(rows) < 25; id++ {
			if stamp.Before(since) {
				continue
			}
			rows = append(rows, map[string]any{"id": id, "load_id": fmt.Sprintf("MOD-%d", id), "status": "delivered", "total_pay": total, "load_pay": total, "total_miles": miles, "pickup_time": "2026-10-19T00:01:00Z", "modified_datetime": stamp.Format(time.RFC3339Nano), "trips": []map[string]any{{"id": id, "total_load_pay": gross, "driver__full_name": "Modified Driver", "mile": miles - 50, "empty_mile": 50}}})
		}
		count := 0
		if !stamp.Before(since) {
			count = max(28-max(first, 1), 0)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": rows, "count": count})
	}))
	defer server.Close()
	// Redirect this test's production client to the local fake provider. No live API.
	target, _ := url.Parse(server.URL)
	transport := http.DefaultTransport
	http.DefaultTransport = modifiedTestTransport{target, transport}
	defer func() { http.DefaultTransport = transport }()
	client := datatruck.NewClient("fixture", "fixture")
	repo := repository.NewLoadRepository(pool)
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if out, err := job.RunModified(ctx); err != nil || out.Fetched != 27 {
		t.Fatalf("first sweep: %+v %v", out, err)
	}
	if id, err := repo.MaxLoadID(ctx); err != nil || id != 26 {
		t.Fatalf("modified sweep advanced discovery: %d %v", id, err)
	}
	var stale repository.LoadRecord
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT raw_payload FROM loads WHERE id=1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var load datatruck.Load
	if err = json.Unmarshal(payload, &load); err != nil {
		t.Fatal(err)
	}
	oldStamp := stamp
	load.ModifiedDatetime = &oldStamp
	stale, err = repository.LoadToRecord(load, payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	readRevision := func() (int64, string) {
		t.Helper()
		var rev int64
		var xmin string
		if err := pool.QueryRow(ctx, `SELECT (SELECT revision FROM payroll_cache_revisions WHERE table_name='loads'),xmin::text FROM loads WHERE id=1`).Scan(&rev, &xmin); err != nil {
			t.Fatal(err)
		}
		return rev, xmin
	}
	rev, xmin := readRevision()
	// A source write error must roll back both values and transport version.
	broken := stale
	advanced := oldStamp.Add(time.Second)
	broken.ModifiedDatetime, broken.TotalPay = &advanced, "invalid numeric"
	if err = repo.RefreshLoads(ctx, []repository.LoadRecord{broken}); err == nil {
		t.Fatal("expected invalid source write to fail")
	}
	var storedVersion time.Time
	if err = pool.QueryRow(ctx, `SELECT modified_datetime FROM datatruck_load_versions WHERE load_id=1`).Scan(&storedVersion); err != nil || !storedVersion.Equal(oldStamp) {
		t.Fatal("failed source write advanced its version", storedVersion, err)
	}
	// Only transport metadata changes: no source row rewrite or payroll revision.
	mu.Lock()
	stamp = stamp.Add(time.Second)
	mu.Unlock()
	if _, err = job.RunModified(ctx); err != nil {
		t.Fatal(err)
	}
	if next, nextXmin := readRevision(); next != rev || nextXmin != xmin {
		t.Fatal("unchanged feed rewrote load or invalidated payroll")
	}
	mu.Lock()
	stamp = stamp.Add(time.Second)
	gross, miles, total = 2100, 600, 2900
	mu.Unlock()
	if _, err = job.RunModified(ctx); err != nil {
		t.Fatal(err)
	}
	for _, write := range []func(context.Context, []repository.LoadRecord) error{repo.RefreshLoads, repo.ReconcileLoads, repo.UpsertLoads} {
		if err = write(ctx, []repository.LoadRecord{stale}); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.UpdateLoadDetails(ctx, stale); err != nil {
		t.Fatal(err)
	}
	board := repository.NewGrossBoardRepository(pool)
	week, _ := time.Parse(time.DateOnly, "2026-10-19")
	view, err := board.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range view.Entries {
		if row.DriverID == driver && row.LoadNumber == "MOD-1" {
			found = true
			if row.DriverRate != "2300.00" || row.OriginalRate != "2800.00" || row.Miles != "500.00" || row.SystemDriverRate != "2100" || row.SystemOriginalRate != "2900.00" || row.SystemMiles != "600.00" {
				t.Fatalf("manual values or fresh comparisons lost: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("missing board row")
	}
	checkpoint, err := repo.LoadModifiedCheckpoint(ctx, client.LoadSyncScope(), time.Now().Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CompleteModifiedSweep(ctx, client.LoadSyncScope(), checkpoint.Add(-time.Second), time.Now()); err == nil {
		t.Fatal("stale checkpoint accepted")
	}
	if err = repo.CompleteModifiedSweep(ctx, client.LoadSyncScope(), checkpoint, checkpoint.Add(-time.Second)); err == nil {
		t.Fatal("checkpoint regressed")
	}
	mu.Lock()
	stamp = stamp.Add(-48 * time.Hour)
	beforeRequests := requests
	mu.Unlock()
	restarted := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if out, err := restarted.RunModified(ctx); err != nil || out.Fetched != 0 {
		t.Fatalf("restart: %+v %v", out, err)
	}
	mu.Lock()
	afterRequests := requests
	mu.Unlock()
	if afterRequests-beforeRequests != 1 {
		t.Fatal("empty sweep used more than one request")
	}
	var missing int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM load_source_availability WHERE NOT present`).Scan(&missing); err != nil || missing != 0 {
		t.Fatal("filtered omission treated as deletion", err)
	}
}
