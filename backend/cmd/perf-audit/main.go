// perf-audit performs bounded, read-only repository diagnostics. It prints only
// timings, counts, response sizes and hashes, never report data or credentials.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
	"os"
	"sync/atomic"
	"time"
)

type tracer struct{ calls atomic.Int64 }

func (t *tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	t.calls.Add(1)
	return ctx
}
func (t *tracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "diagnostic failed:", e)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "driver", "driver, investor, driver-history, investor-history, fuel, financial, board, gross")
	weekArg := flag.String("week", "2026-10-05", "Monday YYYY-MM-DD")
	runs := flag.Int("runs", 2, "sequential samples (1-5)")
	jit := flag.String("jit", "off", "on or off")
	flag.Parse()
	if *runs < 1 || *runs > 5 || (*jit != "on" && *jit != "off") {
		return fmt.Errorf("invalid bounded options")
	}
	week, e := time.Parse(time.DateOnly, *weekArg)
	if e != nil {
		return e
	}
	dsn := os.Getenv("MSERP_DIAGNOSTIC_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("MSERP_DIAGNOSTIC_DATABASE_URL required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return fmt.Errorf("invalid database configuration")
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "25000"
	cfg.ConnConfig.RuntimeParams["jit"] = *jit
	if role := os.Getenv("MSERP_DIAGNOSTIC_ROLE"); role != "" {
		cfg.ConnConfig.RuntimeParams["role"] = role
	}
	t := &tracer{}
	cfg.ConnConfig.Tracer = t
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*runs)*60*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return fmt.Errorf("connection setup failed")
	}
	defer pool.Close()
	pay := repository.NewDriverPayRepository(pool)
	var driver, owner string
	if *mode == "driver-history" {
		if e = pool.QueryRow(ctx, `SELECT driver_id::text FROM gross_board_entries GROUP BY driver_id ORDER BY count(*) DESC,driver_id LIMIT 1`).Scan(&driver); e != nil {
			return e
		}
	}
	if *mode == "investor-history" {
		if e = pool.QueryRow(ctx, `SELECT owner_id::text FROM truck_settlement_terms GROUP BY owner_id ORDER BY min(week_start),count(*) DESC,owner_id LIMIT 1`).Scan(&owner); e != nil {
			return e
		}
	}
	for i := 0; i < *runs; i++ {
		t.calls.Store(0)
		start := time.Now()
		var value any
		switch *mode {
		case "driver":
			value, e = pay.Get(ctx, week)
		case "investor":
			value, e = pay.InvestorPay(ctx, week)
		case "driver-history":
			value, e = pay.History(ctx, driver, repository.Pagination{Page: 1, PageSize: 10})
		case "investor-history":
			value, e = pay.InvestorHistory(ctx, owner, repository.Pagination{Page: 1, PageSize: 10})
		case "fuel":
			value, e = repository.NewFuelRepository(pool).GetDashboard(ctx, repository.FuelDashboardQuery{Year: week.Year(), MapDateFrom: time.Date(week.Year(), 1, 1, 0, 0, 0, 0, time.UTC), MapDateTo: week.AddDate(0, 0, 3)})
		case "financial":
			end := week.AddDate(0, 0, 6)
			value, e = repository.NewDashboardRepository(pool).GetFinancialDashboard(ctx, repository.FinancialDashboardQuery{DateFrom: &week, DateTo: &end})
		case "board":
			value, e = repository.NewDriverBoardRepository(pool).Get(ctx, week)
		case "gross":
			value, e = repository.NewGrossBoardRepository(pool).Get(ctx, week)
		default:
			return fmt.Errorf("unknown mode")
		}
		elapsed := time.Since(start)
		if e != nil {
			return e
		}
		body, e := json.Marshal(value)
		if e != nil {
			return e
		}
		var compressed bytes.Buffer
		z := gzip.NewWriter(&compressed)
		_, _ = z.Write(body)
		_ = z.Close()
		out := map[string]any{"mode": *mode, "sample": i + 1, "ms": float64(elapsed.Microseconds()) / 1000, "db_calls": t.calls.Load(), "bytes": len(body), "gzip_bytes": compressed.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(body)), "jit": *jit}
		if board, ok := value.(repository.DriverBoard); ok {
			compact, err := json.Marshal(repository.CompactBoard(board))
			if err != nil {
				return err
			}
			var packed bytes.Buffer
			writer := gzip.NewWriter(&packed)
			_, _ = writer.Write(compact)
			_ = writer.Close()
			out["compact_bytes"] = len(compact)
			out["compact_gzip_bytes"] = packed.Len()
		}
		if e = json.NewEncoder(os.Stdout).Encode(out); e != nil {
			return e
		}
	}
	return nil
}
