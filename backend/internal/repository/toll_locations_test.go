package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
)

func TestNormalizeTollState(t *testing.T) {
	for source, want := range map[string]string{" oh ": "OH", "West Virginia": "WV", "New York": "NY", "KY/IN": "KY/IN", "": ""} {
		if got := normalizeTollState(source); got != want {
			t.Errorf("%q => %q, want %q", source, got, want)
		}
	}
	args := tollLocationArgs("production", prepass.Transaction{TollAgencyState: "NY", PlateState: "TX"}, time.Now())
	if *(args[2].(*string)) != "NY" {
		t.Fatal("must use agency state, never plate state")
	}
}

func TestTollLocationDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_TOLL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_TOLL_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database config")
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("could not connect to test database")
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE tolls (
		prepass_environment text, prepass_toll_id bigint, amount numeric,
		toll_agency_state text, toll_agency_name text, entry_plaza_name text, exit_plaza_name text, location_synced_at timestamptz);
		INSERT INTO tolls(prepass_environment,prepass_toll_id,amount) VALUES ('production',42,12.30), ('nonproduction',42,99);`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewTollRepository(pool)
	updated, err := repo.UpdateTollLocations(ctx, "production", []prepass.Transaction{{TollID: 42, TollAgencyState: "Ohio", TollAgencyName: "Ohio Turnpike", EntryPlazaName: "Entry", ExitPlazaName: "Exit"}, {TollID: 99}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("updated %d", updated)
	}
	// A later response omitting metadata must not erase previously supplied values.
	_, err = repo.UpdateTollLocations(ctx, "production", []prepass.Transaction{{TollID: 42}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var state, name, entry, exit, amount string
	if err := pool.QueryRow(ctx, `SELECT toll_agency_state,toll_agency_name,entry_plaza_name,exit_plaza_name,amount::text FROM tolls WHERE prepass_environment='production'`).Scan(&state, &name, &entry, &exit, &amount); err != nil {
		t.Fatal(err)
	}
	if state != "OH" || name != "Ohio Turnpike" || entry != "Entry" || exit != "Exit" || amount != "12.30" {
		t.Fatalf("unexpected metadata or changed amount: %s %s %s %s %s", state, name, entry, exit, amount)
	}
	var untouched int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tolls WHERE prepass_environment='nonproduction' AND toll_agency_state IS NULL AND amount=99`).Scan(&untouched); err != nil {
		t.Fatal(err)
	}
	if untouched != 1 {
		t.Fatal("other environment changed")
	}
}
