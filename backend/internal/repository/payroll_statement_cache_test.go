package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPayrollStatementCacheEvictionAndDeduplication(t *testing.T) {
	ctx := context.WithValue(context.Background(), payrollSnapshotKey{}, payrollSnapshot{value: "committed"})
	week := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	repo := &DriverPayRepository{}
	fills := 0
	read := func() (DriverPayWeek, error) { fills++; return DriverPayWeek{WeekStart: "2026-10-05"}, nil }
	get := func(key string) {
		t.Helper()
		if _, e := repo.cachedWeek(ctx, nil, key, week, read); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 256; i++ {
		get(fmt.Sprint(i))
	}
	get("0") // Keep a frequently expanded statement through pressure.
	get("256")
	before := fills
	get("0")
	get("255")
	if fills != before {
		t.Fatal("eviction cleared unrelated hot statements")
	}
	get("1")
	if fills != before+1 || len(repo.cache.entries) != 256 {
		t.Fatal("oldest statement was not individually evicted")
	}
	repo = &DriverPayRepository{}
	var calculations atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	readConcurrent := func() (DriverPayWeek, error) {
		if calculations.Add(1) == 1 {
			close(started)
		}
		<-release
		return DriverPayWeek{Revision: "same"}, nil
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := repo.cachedWeek(ctx, nil, "detail:driver:a", week, readConcurrent)
			results <- e
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	if calculations.Load() != 1 {
		t.Fatal("concurrent identical statement requests recalculated", calculations.Load())
	}
}
