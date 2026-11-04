// reconcile-loads previews and applies explicitly reviewed missing-source repairs.
// Provider calls are read-only; financial writes use native guards/publication.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/joho/godotenv"
	"mserp/internal/datatruck"
	"mserp/internal/db"
	"mserp/internal/repository"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	since := flag.String("since", "", "earliest service date YYYY-MM-DD")
	preview := flag.String("preview", "", "write a read-only JSON preview to this private file")
	refresh := flag.Bool("refresh-current", false, "explicitly refresh returned current source records before creating a preview")
	apply := flag.String("apply", "", "apply this exact reviewed JSON preview")
	actor := flag.String("actor", "", "administrator user UUID for the immutable receipt")
	request := flag.String("request", "", "unique UUID; reuse only for exact retry")
	flag.Parse()
	if (*preview == "") == (*apply == "") {
		return fmt.Errorf("choose exactly one of -preview or -apply")
	}
	if *refresh && *apply != "" {
		return fmt.Errorf("refresh-current is allowed only before a new preview")
	}
	for _, path := range []string{".env.local", ".env", "/etc/mserp/mserp.env"} {
		_ = godotenv.Load(path)
	}
	databaseURL, apiKey, company := os.Getenv("DATABASE_URL"), os.Getenv("DATATRUCK_API_KEY"), os.Getenv("DATATRUCK_COMPANY_NAME")
	if databaseURL == "" || apiKey == "" || company == "" {
		return fmt.Errorf("DATABASE_URL and DataTruck credentials are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("database setup failed")
	}
	defer pool.Close()
	loadRepo := repository.NewLoadRepository(pool)
	board := repository.NewGrossBoardRepository(pool)
	var plan repository.LoadReconciliationPlan
	var ids []int
	if *apply != "" {
		if *actor == "" || *request == "" {
			return fmt.Errorf("apply requires actor and request UUIDs")
		}
		body, e := os.ReadFile(*apply)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(body, &plan); e != nil {
			return e
		}
		*since = plan.Since
		for _, g := range plan.Groups {
			for _, s := range g.Sources {
				ids = append(ids, s.ID)
			}
		}
	} else {
		if _, err = time.Parse(time.DateOnly, *since); err != nil {
			return fmt.Errorf("valid -since date required")
		}
		ids, err = loadRepo.ReconciliationIDs(ctx, *since)
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 || len(ids) > 500 {
		return fmt.Errorf("expected 1-500 candidate records, got %d", len(ids))
	}
	sort.Ints(ids)
	for i, id := range ids {
		if id <= 0 || (i > 0 && ids[i-1] == id) {
			return fmt.Errorf("invalid or repeated candidate ID")
		}
	}
	client := datatruck.NewClient(apiKey, company)
	live := []datatruck.Load{}
	for start := 0; start < len(ids); start += 25 {
		rows, e := client.FetchLoadsByIDs(ctx, ids[start:min(start+25, len(ids))])
		if e != nil {
			return e
		}
		live = append(live, rows...)
	}
	if *apply != "" {
		plan, err = board.ApplyLoadReconciliation(ctx, plan, live, *actor, *request)
		if err != nil {
			return err
		}
	} else {
		if *refresh {
			records := make([]repository.LoadRecord, 0, len(live))
			for _, load := range live {
				body, e := json.Marshal(load)
				if e != nil {
					return e
				}
				record, e := repository.LoadToRecord(load, body, time.Now())
				if e != nil {
					return e
				}
				records = append(records, record)
			}
			if err = loadRepo.RefreshLoads(ctx, records); err != nil {
				return err
			}
			returned := make([]int, 0, len(live))
			for _, load := range live {
				returned = append(returned, load.ID)
			}
			if err = loadRepo.RecordLoadSourceLookup(ctx, ids, returned); err != nil {
				return err
			}
		}
		plan, err = board.PreviewLoadReconciliation(ctx, *since, ids, live)
		if err != nil {
			return err
		}
		body, e := json.MarshalIndent(plan, "", "  ")
		if e != nil {
			return e
		}
		file, e := os.OpenFile(*preview, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = file.Write(body)
		closeErr := file.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	repairs, held := 0, 0
	for _, g := range plan.Groups {
		if g.Hold != "" {
			held++
		} else if len(g.After) > 0 {
			repairs++
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"digest": plan.Digest, "groups": len(plan.Groups), "repairs": repairs, "held": held, "applied": *apply != ""})
}
