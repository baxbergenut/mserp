package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/prepass"
	"mserp/internal/repository"
)

func TestSyncTollsAgainstPrePass(t *testing.T) {
	databaseURL := os.Getenv("PREPASS_INTEGRATION_DATABASE_URL")
	clientID := os.Getenv("PREPASS_INTEGRATION_CLIENT_ID")
	clientSecret := os.Getenv("PREPASS_INTEGRATION_CLIENT_SECRET")
	if databaseURL == "" || clientID == "" || clientSecret == "" {
		t.Skip("PrePass integration test credentials are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	client := prepass.NewClient("https://api.prepass.com", clientID, clientSecret)
	repo := repository.NewTollRepository(pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	startDate := time.Now().UTC().AddDate(0, 0, -7)
	transactions, err := client.FetchTransactions(
		ctx,
		utcDay(startDate),
		utcDay(time.Now()).AddDate(0, 0, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) == 0 {
		t.Fatal("PrePass returned no recent tolls")
	}
	seed := transactions[0]
	postingAt, err := prepass.ParseTimestamp(seed.PostDateTime)
	if err != nil {
		t.Fatal(err)
	}
	invoiceAt, err := prepass.ParseTimestamp(seed.InvoiceDateTime)
	if err != nil {
		t.Fatal(err)
	}
	exitAt, err := prepass.ParseTimestamp(seed.ExitDateTime)
	if err != nil {
		t.Fatal(err)
	}
	var legacyID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tolls (
			report_id, truck_id, posting_date, invoice_date, customer_id,
			source, read_type, transponder_or_plate, equipment_unit, agency,
			exit_plaza, exit_date, exit_time, toll_class, amount, row_fingerprint
		) VALUES (
			NULL, NULL, $1, $2, $3, 'legacy', 'legacy', 'legacy', $4,
			'legacy', 'legacy', $5, $6, 'legacy', $7::numeric, $8
		)
		RETURNING id`,
		time.Date(postingAt.Year(), postingAt.Month(), postingAt.Day(), 0, 0, 0, 0, time.UTC),
		time.Date(invoiceAt.Year(), invoiceAt.Month(), invoiceAt.Day(), 0, 0, 0, 0, time.UTC),
		seed.AccountNumber.String(),
		strings.ToUpper(strings.Join(strings.Fields(seed.VehicleNumber), " ")),
		time.Date(exitAt.Year(), exitAt.Month(), exitAt.Day(), 0, 0, 0, 0, time.UTC),
		exitAt.Format("15:04:05"),
		seed.TollCharge.String(),
		strings.Repeat("0", 64),
	).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}

	job := NewSyncTollsJob(
		client,
		repo,
		"production",
		startDate,
		logger,
	)
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched == 0 || result.Saved != result.Fetched {
		t.Fatalf("sync result = %+v", result)
	}

	var stored int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM tolls
		WHERE prepass_environment = 'production'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != result.Fetched {
		t.Fatalf("stored %d tolls after fetching %d; legacy row was duplicated", stored, result.Fetched)
	}
	var attached bool
	if err := pool.QueryRow(ctx, `
		SELECT prepass_toll_id IS NOT NULL
		FROM tolls
		WHERE id = $1`, legacyID).Scan(&attached); err != nil {
		t.Fatal(err)
	}
	if !attached {
		t.Fatal("legacy CSV toll was not attached to its PrePass toll ID")
	}
}
