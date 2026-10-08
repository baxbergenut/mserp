package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPayrollQueriesDoNotGrowPerDriver(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("test database required")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("isolated test database required")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("payroll_budget_%d", time.Now().UnixNano())
	schema := pgx.Identifier{name}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, e := admin.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE SCHEMA ` + schema + `;SET search_path TO ` + schema + `,public`)
	defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+schema+` CASCADE`) }()
	source, e := os.ReadFile("../../sql/init.sql")
	if e != nil {
		t.Fatal(e)
	}
	exec(string(source))
	exec(`GRANT USAGE ON SCHEMA ` + schema + ` TO mserp_app;GRANT SELECT ON ALL TABLES IN SCHEMA ` + schema + ` TO mserp_app;
 INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Budget First','budget first','cpm',0.75)`)
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name + ",public"
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	cfg.ConnConfig.Tracer = db.Tracer{}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	repo := NewDriverPayRepository(pool)
	week, _ := time.Parse(time.DateOnly, ChargeCurrentWeek())
	read := func() int64 {
		t.Helper()
		traced, m := db.WithMetrics(ctx)
		if _, e := repo.Get(traced, week); e != nil {
			t.Fatal(e)
		}
		return m.Queries.Load()
	}
	one := read()
	exec(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) SELECT 'Budget '||n,'budget '||n,'cpm',0.75 FROM generate_series(1,40)n`)
	many := read()
	if one < 5 || many > one+2 || many > 50 {
		t.Fatalf("payroll query growth: one driver=%d, 41 drivers=%d", one, many)
	}
	// Concurrent tests may commit unrelated transactions between calls. Cache
	// reuse is tested against a pinned snapshot in TestPayrollSnapshotCacheDatabase.
}
