package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverNamesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_RELAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/104_driver_name_parts.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(init), "\r\n", "\n")
	body := strings.ReplaceAll(string(migration), "\r\n", "\n")
	if !strings.Contains(source, body) {
		t.Fatal("init must include exact migration")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_names_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public`)
			defer admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`)
			if mode == "migration" {
				exec(strings.Replace(source, body, "", 1))
				exec(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Surname Given Middle','surname given middle','cpm',0)`)
				exec(body)
				var unchanged bool
				if err := admin.QueryRow(ctx, `SELECT full_name='Surname Given Middle' AND first_name IS NULL AND last_name IS NULL FROM drivers WHERE normalized_name='surname given middle'`).Scan(&unchanged); err != nil || !unchanged {
					t.Fatal("migration guessed or changed name", err)
				}
			} else {
				exec(source)
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			fleet := NewFleetRepository(pool)
			first, last := "Ana María", "de la Cruz"
			input := DriverInput{FullName: "old ordering", FirstName: &first, LastName: &last, PayType: "cpm", Active: true, EscrowAmount: "0"}
			driver, e := fleet.CreateDriver(ctx, input)
			if e != nil {
				t.Fatal(e)
			}
			if driver.FullName != "Ana María de la Cruz" || driver.FirstName == nil || *driver.FirstName != first || driver.LastName == nil || *driver.LastName != last {
				t.Fatalf("structured create: %+v", driver)
			}
			input.FirstName, input.LastName = nil, nil
			input.FullName = "Incorrect Legacy Name"
			driver, e = fleet.UpdateDriver(ctx, driver.ID, input)
			if e != nil || driver.FullName != "Ana María de la Cruz" {
				t.Fatal("legacy writer overwrote reviewed name", e, driver.FullName)
			}
			first = "Ana"
			input.FirstName, input.LastName = &first, &last
			driver, e = fleet.UpdateDriver(ctx, driver.ID, input)
			if e != nil || driver.FullName != "Ana de la Cruz" {
				t.Fatal("structured edit", e, driver.FullName)
			}
			var normalized string
			if e = pool.QueryRow(ctx, `SELECT normalized_name FROM drivers WHERE id=$1`, driver.ID).Scan(&normalized); e != nil || normalized != "ana de la cruz" {
				t.Fatal("matching name stale", e, normalized)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,first_name) VALUES('Partial','partial','cpm',0,'Only')`); e == nil {
				t.Fatal("partial split accepted")
			}
		})
	}
}

func TestConfirmedProviderNameParts(t *testing.T) {
	first, last := driverProviderName("Ana María de la Cruz", "Ana María", "de la Cruz")
	if first != "Ana María" || last != "de la Cruz" {
		t.Fatal("confirmed compound name split again")
	}
	driver, err := wmtDriver("Ana María de la Cruz", "ana@example.test", "5555550100", first, last)
	if err != nil || driver.FirstName != first || driver.LastName != last {
		t.Fatal("provider ignored confirmed name", driver, err)
	}
}
