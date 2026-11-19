package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestLoadRefreshBatchDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		const size = 250
		driver, dispatcher := "Batch Driver", "Batch Dispatcher"
		_, err := pool.Exec(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Batch Driver','batch driver','cpm',0);
 INSERT INTO dispatchers(full_name,normalized_name) VALUES('Batch Dispatcher','batch dispatcher')`)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		records := make([]LoadRecord, size)
		for i := range records {
			records[i] = LoadRecord{ID: 70000 + i, LoadID: fmt.Sprintf("B-%d", i), DriverName: &driver, DispatcherName: &dispatcher,
				Status: "booked", LoadPay: "123.45", TotalOtherPay: "0", TotalPay: "123.45", RawPayload: []byte(`{"stops":[]}`), SyncedAt: now, ModifiedDatetime: &now}
		}
		repo := NewLoadRepository(pool)
		run, metrics := db.WithMetrics(ctx)
		start := time.Now()
		if err = repo.ReconcileLoads(run, records); err != nil {
			t.Fatal(err)
		}
		t.Logf("reconciled %d loads: %d SQL calls in %v", size, metrics.Queries.Load(), time.Since(start))
		var linked int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM loads l JOIN drivers d ON d.id=l.driver_id JOIN dispatchers p ON p.id=l.dispatcher_id JOIN datatruck_load_versions v ON v.load_id=l.id WHERE d.full_name=$1 AND p.full_name=$2 AND l.total_pay=123.45 AND v.modified_datetime=$3`, driver, dispatcher, now).Scan(&linked); err != nil || linked != size {
			t.Fatal(linked, err)
		}

		// Ambiguous compatible names stay unlinked. A unique compatible match
		// retains the same driver even if the provider reverses the name.
		if _, err = pool.Exec(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES
 ('Alex Carter','alex carter','cpm',0),('Alex James Carter','alex james carter','cpm',0)`); err != nil {
			t.Fatal(err)
		}
		uniqueName, ambiguousName := "Driver, Batch", "Carter, Alex"
		unique, ambiguous := records[0], records[1]
		unique.DriverName, ambiguous.DriverName = &uniqueName, &ambiguousName
		if err = repo.RefreshLoads(ctx, []LoadRecord{unique, ambiguous}); err != nil {
			t.Fatal(err)
		}
		var matched bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM loads l JOIN drivers d ON d.id=l.driver_id WHERE l.id=$1 AND d.full_name=$3)
 AND EXISTS(SELECT 1 FROM loads WHERE id=$2 AND driver_id IS NULL)`, unique.ID, ambiguous.ID, driver).Scan(&matched); err != nil || !matched {
			t.Fatal("compatible name resolution", matched, err)
		}

		// Existing-only refresh must ignore unseen IDs. Multiple observations
		// of one load apply in order while the version fence stays monotonic.
		unseen := records[0]
		unseen.ID = 80000
		later := now.Add(time.Hour)
		first, last := records[1], records[1]
		first.ModifiedDatetime = &later
		last.TotalPay = "198.22"
		refresh := append(append([]LoadRecord{}, records...), first, last, unseen)
		run, reads := db.WithMetrics(ctx)
		start = time.Now()
		if err = repo.RefreshLoads(run, refresh); err != nil {
			t.Fatal(err)
		}
		t.Logf("refreshed %d observations: %d SQL calls in %v", len(refresh), reads.Queries.Load(), time.Since(start))
		var ok bool
		if err = pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM loads WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM datatruck_load_versions WHERE load_id=$1)
 AND EXISTS(SELECT 1 FROM loads JOIN datatruck_load_versions v ON v.load_id=loads.id WHERE loads.id=$2 AND total_pay=198.22 AND modified_datetime=$3)`, unseen.ID, last.ID, later).Scan(&ok); err != nil || !ok {
			t.Fatal("existing-only, repeat or version handling", ok, err)
		}
		if metrics.Queries.Load() > 30 || reads.Queries.Load() > 30 {
			t.Fatal("load refresh issued per-record SQL", metrics.Queries.Load(), reads.Queries.Load())
		}
	})
}
