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
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
	"mserp/internal/repository"
	"os"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "diagnostic failed:", e)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "driver", "driver, investor, driver-statement, investor-statement, driver-summary, investor-summary, driver-history, investor-history, fuel, financial, board, gross")
	weekArg := flag.String("week", "2026-10-05", "Monday YYYY-MM-DD")
	runs := flag.Int("runs", 2, "sequential samples (1-5)")
	explain := flag.Bool("explain", false, "bounded read-only plan for the slowest query, with expressions removed")
	jit := flag.String("jit", "off", "on or off")
	historicalRoot := flag.Bool("historical-root", false, "select a statement with the most historical assignment relationships")
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
	planTrace := &planTracer{}
	cfg.ConnConfig.Tracer = db.Tracer{}
	if *explain {
		cfg.ConnConfig.Tracer = planTrace
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*runs)*60*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return fmt.Errorf("connection setup failed")
	}
	defer pool.Close()
	pay := repository.NewDriverPayRepository(pool)
	var driver, owner, truck string
	if *mode == "driver-statement" {
		query := `SELECT driver_id::text FROM gross_board_entries WHERE service_date >= $1::date AND service_date < $1::date+7 GROUP BY driver_id ORDER BY count(*) DESC,driver_id LIMIT 1`
		if *historicalRoot {
			query = `SELECT a.driver_id::text FROM truck_driver_assignments a WHERE EXISTS(SELECT 1 FROM gross_board_entries b WHERE b.driver_id=a.driver_id AND b.service_date >= $1::date AND b.service_date < $1::date+7) GROUP BY a.driver_id ORDER BY count(DISTINCT a.truck_id) DESC,a.driver_id LIMIT 1`
		}
		if e = pool.QueryRow(ctx, query, week).Scan(&driver); e != nil {
			return e
		}
	}
	if *mode == "investor-statement" {
		query := `SELECT truck_id::text FROM truck_settlement_terms WHERE week_start <= $1::date GROUP BY truck_id ORDER BY count(*) DESC,truck_id LIMIT 1`
		if *historicalRoot {
			query = `SELECT a.truck_id::text FROM truck_driver_assignments a WHERE EXISTS(SELECT 1 FROM truck_settlement_terms t WHERE t.truck_id=a.truck_id AND t.week_start <= $1::date) GROUP BY a.truck_id ORDER BY count(DISTINCT a.driver_id) DESC,a.truck_id LIMIT 1`
		}
		if e = pool.QueryRow(ctx, query, week).Scan(&truck); e != nil {
			return e
		}
	}
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
		sampleCtx, metrics := db.WithQueryMetrics(ctx)
		start := time.Now()
		var value any
		switch *mode {
		case "driver":
			value, e = pay.Get(sampleCtx, week)
		case "investor":
			value, e = pay.InvestorPay(sampleCtx, week)
		case "driver-statement":
			value, e = pay.PayrollStatement(sampleCtx, "driver", week, driver)
		case "investor-statement":
			value, e = pay.PayrollStatement(sampleCtx, "investor", week, truck)
		case "driver-summary":
			value, e = pay.PayrollPreview(sampleCtx, "driver", week)
		case "investor-summary":
			value, e = pay.PayrollPreview(sampleCtx, "investor", week)
		case "driver-history":
			value, e = pay.History(sampleCtx, driver, repository.Pagination{Page: 1, PageSize: 10})
		case "investor-history":
			value, e = pay.InvestorHistory(sampleCtx, owner, repository.Pagination{Page: 1, PageSize: 10})
		case "fuel":
			value, e = repository.NewFuelRepository(pool).GetDashboard(sampleCtx, repository.FuelDashboardQuery{Year: week.Year(), MapDateFrom: time.Date(week.Year(), 1, 1, 0, 0, 0, 0, time.UTC), MapDateTo: week.AddDate(0, 0, 3)})
		case "financial":
			end := week.AddDate(0, 0, 6)
			value, e = repository.NewDashboardRepository(pool).GetFinancialDashboard(sampleCtx, repository.FinancialDashboardQuery{DateFrom: &week, DateTo: &end})
		case "board":
			value, e = repository.NewDriverBoardRepository(pool).Get(sampleCtx, week)
		case "gross":
			value, e = repository.NewGrossBoardRepository(pool).Get(sampleCtx, week)
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
		out := map[string]any{"mode": *mode, "sample": i + 1, "ms": float64(elapsed.Microseconds()) / 1000, "db_calls": metrics.Queries.Load(), "bytes": len(body), "gzip_bytes": compressed.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(body)), "jit": *jit}
		out["historical_root"] = *historicalRoot
		out["phases_ms"], out["queries"] = metrics.Details()
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
		if report, ok := value.(repository.DriverPayWeek); ok {
			full, err := json.Marshal(repository.PaginatePay(report, repository.PayPageQuery{Pagination: repository.Pagination{Page: 1, PageSize: 25}}))
			if err != nil {
				return err
			}
			summary, err := json.Marshal(repository.PaginatePay(report, repository.PayPageQuery{Pagination: repository.Pagination{Page: 1, PageSize: 25}, Summary: true}))
			if err != nil {
				return err
			}
			out["page_bytes"], out["summary_bytes"] = len(full), len(summary)
		}
		if *explain && i == 0 {
			out["plan"], e = planTrace.explain(ctx, pool)
			if e != nil {
				return e
			}
		}
		if e = json.NewEncoder(os.Stdout).Encode(out); e != nil {
			return e
		}
	}
	return nil
}
