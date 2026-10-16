// Export the opt-in expense outbox to Google Sheets. Dry run is the default.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2/google"
	"mserp/internal/expensesheet"
)

func run(ctx context.Context, apply, initialize bool) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return errors.New("cannot connect to the MSERP database")
	}
	defer conn.Close(context.Background())
	var locked bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended('mserp:expense-sheet-export',0))").Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("another expense sheet exporter is already running")
	}
	var enabled bool
	var spreadsheetID string
	var maintenanceID, hrID, safetyID int
	if err = conn.QueryRow(ctx, "SELECT enabled,spreadsheet_id,maintenance_sheet_id,hr_sheet_id,safety_sheet_id FROM expense_sheet_export_config WHERE singleton").Scan(&enabled, &spreadsheetID, &maintenanceID, &hrID, &safetyID); err != nil {
		return errors.New("expense sheet export configuration is missing; apply migration 105")
	}
	if !initialize && !enabled {
		log.Print("expense sheet export is disabled")
		return nil
	}
	// Bound each batch; watch mode drains further batches immediately.
	rows, err := conn.Query(ctx, "SELECT expense_id::text,spreadsheet_id,sheet_id,payload FROM expense_sheet_exports WHERE delivered_at IS NULL ORDER BY created_at,expense_id LIMIT 10")
	if err != nil {
		return err
	}
	entries := []expensesheet.Entry{}
	for rows.Next() {
		var e expensesheet.Entry
		if err = rows.Scan(&e.ID, &e.Spreadsheet, &e.SheetID, &e.Payload); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !apply && !initialize {
		log.Printf("dry run: %d pending entries in next batch; no Google or database writes", len(entries))
		return nil
	}
	if !initialize && len(entries) == 0 {
		return nil
	}
	key, err := os.ReadFile(os.Getenv("EXPENSE_SHEET_CREDENTIALS_FILE"))
	if err != nil {
		return errors.New("EXPENSE_SHEET_CREDENTIALS_FILE must name a readable service-account JSON key")
	}
	var identity struct {
		Type     string `json:"type"`
		TokenURI string `json:"token_uri"`
	}
	if json.Unmarshal(key, &identity) != nil || identity.Type != "service_account" || identity.TokenURI != "https://oauth2.googleapis.com/token" {
		return errors.New("expected a Google service-account key using the official OAuth token endpoint")
	}
	credentials, err := google.JWTConfigFromJSON(key, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		return errors.New("invalid Google service-account credentials")
	}
	httpClient := credentials.Client(ctx)
	httpClient.Transport = &pacedTransport{base: httpClient.Transport, gate: &sheetRequests}
	httpClient.Timeout = 30 * time.Second
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client := expensesheet.Client{HTTP: httpClient}
	tabs, err := client.Prepare(ctx, spreadsheetID, maintenanceID, hrID, safetyID, initialize, apply)
	if err != nil {
		return err
	}
	if initialize {
		log.Printf("sheet export headers verified (apply=%t); no expense rows exported", apply)
		return nil
	}
	failed := 0
	delivered := 0
	for _, e := range entries {
		if e.Spreadsheet != spreadsheetID {
			return errors.New("pending snapshot is pinned to a different spreadsheet; restore its original configuration")
		}
		tab, ok := tabs[e.SheetID]
		if !ok {
			return errors.New("pending snapshot is pinned to an unconfigured sheet")
		}
		if _, err = conn.Exec(ctx, "UPDATE expense_sheet_exports SET attempts=attempts+1,last_attempt_at=now() WHERE expense_id=$1 AND delivered_at IS NULL", e.ID); err != nil {
			return err
		}
		if err = client.Deliver(ctx, e, tab); err != nil {
			failed++
			log.Printf("expense %s remains queued: %v", e.ID, err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if _, err = conn.Exec(ctx, "UPDATE expense_sheet_exports SET delivered_at=now() WHERE expense_id=$1 AND delivered_at IS NULL", e.ID); err != nil {
			return err
		}
		delivered++
		tabs[e.SheetID] = tab
	}
	log.Printf("expense sheet export: %d delivered, %d retained for retry", delivered, failed)
	if failed > 0 {
		return errors.New("some exports remain queued; inspect the service journal")
	}
	return nil
}
func main() {
	apply := flag.Bool("apply", false, "Write to Google Sheets and record confirmed deliveries")
	initialize := flag.Bool("initialize", false, "Verify/add MSERP export headers only; requires -apply to write")
	watch := flag.Bool("watch", false, "Deliver on committed expense notifications, with startup recovery and minute retries; requires -apply")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var err error
	if *watch {
		if !*apply || *initialize {
			err = errors.New("-watch requires -apply and cannot be combined with -initialize")
		} else {
			err = watchExports(ctx, os.Getenv("DATABASE_URL"), time.Minute, func(ctx context.Context) error { return run(ctx, true, false) })
		}
	} else {
		batchCtx, batchCancel := context.WithTimeout(ctx, 4*time.Minute)
		defer batchCancel()
		err = run(batchCtx, *apply, *initialize)
	}
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
