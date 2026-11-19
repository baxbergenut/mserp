package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBoardPlanCacheKeepsStopEvidence(t *testing.T) {
	var cache boardWeekCache[map[string][]BoardLoad]
	lat := 43.0
	read := func() (map[string][]BoardLoad, error) {
		return map[string][]BoardLoad{"d": {{queueAlias: "alias", Stops: []BoardStop{{geocode: &boardStopGeocode{state: "TX"}, Latitude: &lat, Schedule: &BoardSchedule{Label: "pickup"}}}}}}, nil
	}
	first, err := cache.get(context.Background(), "one", cloneBoardPlans, read)
	if err != nil {
		t.Fatal(err)
	}
	first["d"][0].Stops[0].geocode.state = "other"
	*first["d"][0].Stops[0].Latitude = 20
	first["d"][0].Stops[0].Schedule.Label = ""
	second, err := cache.get(context.Background(), "one", cloneBoardPlans, func() (map[string][]BoardLoad, error) { t.Fatal("unexpected second read"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	p := second["d"][0]
	stop := p.Stops[0]
	if p.queueAlias != "alias" || stop.geocode == nil || stop.geocode.state != "TX" || *stop.Latitude != 43 || stop.Schedule.Label != "pickup" {
		t.Fatal("cached stop evidence changed")
	}
}

func TestBoardManualSourcesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		var caches boardLoadSourceCaches
		states := map[string]boardLoadState{"driver": {Manual: []BoardLoad{{PlanID: "first", Number: "Manual-Cache-170"}}}}
		read := func() BoardLoad {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			key, err := boardWeekRevision(ctx, tx, time.Now(), false)
			if err != nil || key == "" {
				t.Fatalf("missing revision %q, %v", key, err)
			}
			out, err := caches.readManual(ctx, tx, key, states)
			if err != nil {
				t.Fatal(err)
			}
			return out["driver"][0]
		}
		if p := read(); p.LoadID != nil {
			t.Fatal("unexpected source")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES (1701001,'MANUAL-CACHE-170','Dispatched',0,0,'{}')`); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			p := read()
			if p.LoadID == nil || *p.LoadID != 1701001 || p.Number != "Manual-Cache-170" || p.PlanID != "first" {
				t.Fatal("new source missing on read", i)
			}
		}
		states["driver"] = boardLoadState{Manual: []BoardLoad{{PlanID: "second", Number: "manual-cache-170"}}}
		if p := read(); p.PlanID != "second" || p.Number != "manual-cache-170" {
			t.Fatal("manual queue edit was lost")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES (1701002,'MANUAL-CACHE-170','Dispatched',0,0,'{}')`); err != nil {
			t.Fatal(err)
		}
		if p := read(); p.LoadID != nil {
			t.Fatal("ambiguous source was selected")
		}
	})
}
