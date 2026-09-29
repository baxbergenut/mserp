package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInvestorsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_INVESTOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_INVESTOR_TEST_DATABASE_URL to a disposable _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") {
		t.Fatal("a disposable _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("investors_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			source, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), "CREATE TABLE investors (")
				if !ok {
					t.Fatal("missing boundary")
				}
				exec(before + "COMMIT;")
			} else {
				exec(string(source))
			}
			var driver, truck, companyTruck string
			if err = admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,is_owner_operator,pay_type,pay_rate) VALUES('Owner Driver','owner driver',true,'gross_percentage',80) RETURNING id`).Scan(&driver); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('OWNER-1') RETURNING id`).Scan(&truck); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,is_company_owned) VALUES('COMPANY-1',false) RETURNING id`).Scan(&companyTruck); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id) VALUES($1,$2)`, truck, driver)
			if mode == "migration" {
				m, e := os.ReadFile("../../sql/032_add_investors.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(m))
			}
			// Grant only old tables: new runtime tables must already belong to mserp_app.
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,trucks,truck_driver_assignments,files,dispatchers TO mserp_app`)
			appcfg := cfg.Copy()
			appcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appcfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, appcfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			owners, e := repo.ListInvestors(ctx, "")
			if e != nil {
				t.Fatal(e)
			}
			if len(owners) != map[string]int{"fresh": 1, "migration": 2}[mode] {
				t.Fatalf("owners: %+v", owners)
			}
			company, e := repo.GetTruck(ctx, companyTruck)
			if e != nil || company.OwnerID != CompanyOwnerID || !company.IsCompanyOwned {
				t.Fatalf("company default: %+v %v", company, e)
			}
			var linked Investor
			if mode == "migration" {
				linked = owners[1]
				if linked.DriverID == nil || *linked.DriverID != driver || len(linked.Trucks) != 1 {
					t.Fatalf("backfill: %+v", linked)
				}
			} else {
				linked, e = repo.SaveInvestor(ctx, "", InvestorInput{DriverID: &driver, Active: true})
				if e != nil {
					t.Fatal(e)
				}
			}
			if _, e = repo.SaveInvestor(ctx, "", InvestorInput{DriverID: &driver, Active: true}); e == nil {
				t.Fatal("duplicate driver accepted")
			}
			if _, e = repo.SaveInvestor(ctx, CompanyOwnerID, InvestorInput{FullName: "Changed", Active: true}); !errors.Is(e, ErrInvestorConflict) {
				t.Fatalf("company edited: %v", e)
			}
			independent, e := repo.SaveInvestor(ctx, "", InvestorInput{FullName: "Outside Investor", Active: true})
			if e != nil {
				t.Fatal(e)
			}
			input := TruckInput{UnitNumber: "OWNER-1", OwnerID: &independent.ID, Status: "available", Active: true}
			updated, e := repo.UpdateTruck(ctx, truck, input)
			if e != nil || updated.OwnerID != independent.ID || updated.IsCompanyOwned {
				t.Fatalf("transfer: %+v %v", updated, e)
			}
			historyCount := func() int {
				t.Helper()
				var count int
				if e := pool.QueryRow(ctx, `SELECT count(*) FROM truck_ownership_history WHERE truck_id=$1`, truck).Scan(&count); e != nil {
					t.Fatal(e)
				}
				return count
			}
			count := historyCount()
			input.OwnerID = nil
			input.DriverID = &driver
			updated, e = repo.UpdateTruck(ctx, truck, input)
			if e != nil || updated.OwnerID != independent.ID || historyCount() != count {
				t.Fatalf("driver change altered ownership: %+v %v", updated, e)
			}
			_, e = repo.SaveInvestor(ctx, independent.ID, InvestorInput{FullName: independent.FullName, Active: false})
			if e != nil {
				t.Fatal(e)
			}
			_, e = repo.CreateTruck(ctx, TruckInput{UnitNumber: "INACTIVE", OwnerID: &independent.ID, Status: "available", Active: true})
			if !errors.Is(e, ErrInactiveOwner) {
				t.Fatalf("inactive new owner: %v", e)
			}
			input.OwnerID = &independent.ID
			if _, e = repo.UpdateTruck(ctx, truck, input); e != nil {
				t.Fatalf("retaining inactive owner: %v", e)
			}
			input.OwnerID = &linked.ID
			if _, e = repo.UpdateTruck(ctx, truck, input); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `UPDATE drivers SET full_name='Renamed Driver' WHERE id=$1`, driver); e != nil {
				t.Fatal(e)
			}
			if e = repo.DeleteDriver(ctx, driver); e != nil {
				t.Fatal(e)
			}
			retained, e := repo.GetInvestor(ctx, linked.ID)
			if e != nil || retained.FullName != "Renamed Driver" || retained.DriverID != nil {
				t.Fatalf("deleted driver identity: %+v %v", retained, e)
			}
			if e = repo.DeleteTruck(ctx, truck); e != nil {
				t.Fatal(e)
			}
			var preserved int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM truck_ownership_history WHERE truck_id IS NULL AND unassigned_at IS NOT NULL AND truck_unit='OWNER-1'`).Scan(&preserved); e != nil || preserved != count+1 {
				t.Fatalf("deleted truck history %d %v", preserved, e)
			}
			newTruck, e := repo.CreateTruck(ctx, TruckInput{UnitNumber: "NEW-1", OwnerID: &linked.ID, Status: "available", Active: true})
			if e != nil {
				t.Fatal(e)
			}
			var entries int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM truck_ownership_history WHERE truck_id=$1`, newTruck.ID).Scan(&entries); e != nil || entries != 1 {
				t.Fatalf("new truck history %d %v", entries, e)
			}
		})
	}
}
