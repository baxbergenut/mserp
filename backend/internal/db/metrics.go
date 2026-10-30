package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Metrics contains counts and durations only. SQL, arguments and identities are
// deliberately excluded from application performance logging.
type Metrics struct {
	Queries      atomic.Int64
	QueryNanos   atomic.Int64
	AcquireNanos atomic.Int64
	mu           sync.Mutex
	phases       map[string]int64
	fingerprints map[string]QueryMetric
	events       map[string]int64
}

// Event records only fixed operational categories, never identities or inputs.
func Event(ctx context.Context, name string) {
	if m, ok := ctx.Value(metricsKey{}).(*Metrics); ok {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.events == nil {
			m.events = map[string]int64{}
		}
		m.events[name]++
	}
}

func (m *Metrics) Events() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]int64{}
	for k, v := range m.events {
		result[k] = v
	}
	return result
}

type QueryMetric struct {
	Calls int64   `json:"calls"`
	MS    float64 `json:"ms"`
}

// WithQueryMetrics opts diagnostics into bounded SQL fingerprints. SQL text and
// arguments are never retained or logged, and ordinary requests avoid hashing.
func WithQueryMetrics(ctx context.Context) (context.Context, *Metrics) {
	ctx, m := WithMetrics(ctx)
	m.fingerprints = map[string]QueryMetric{}
	return ctx, m
}
func Phase(ctx context.Context, name string) func() {
	m, _ := ctx.Value(metricsKey{}).(*Metrics)
	if m == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.phases == nil {
			m.phases = map[string]int64{}
		}
		m.phases[name] += time.Since(start).Nanoseconds()
	}
}
func (m *Metrics) Details() (map[string]float64, map[string]QueryMetric) {
	m.mu.Lock()
	defer m.mu.Unlock()
	phases := map[string]float64{}
	queries := map[string]QueryMetric{}
	for k, n := range m.phases {
		phases[k] = float64(n) / 1e6
	}
	for k, q := range m.fingerprints {
		queries[k] = q
	}
	return phases, queries
}

type queryMeasurement struct {
	start       time.Time
	fingerprint string
}
type metricsKey struct{}
type queryStartKey struct{}
type acquireStartKey struct{}

func WithMetrics(ctx context.Context) (context.Context, *Metrics) {
	m := &Metrics{}
	return context.WithValue(ctx, metricsKey{}, m), m
}

type Tracer struct{}

func (Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if m, ok := ctx.Value(metricsKey{}).(*Metrics); ok {
		m.Queries.Add(1)
		q := queryMeasurement{start: time.Now()}
		if m.fingerprints != nil {
			q.fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(data.SQL)))[:16]
		}
		return context.WithValue(ctx, queryStartKey{}, q)
	}
	return ctx
}
func (Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if q, ok := ctx.Value(queryStartKey{}).(queryMeasurement); ok {
		m := ctx.Value(metricsKey{}).(*Metrics)
		elapsed := time.Since(q.start)
		m.QueryNanos.Add(elapsed.Nanoseconds())
		if q.fingerprint != "" {
			m.mu.Lock()
			defer m.mu.Unlock()
			if _, ok := m.fingerprints[q.fingerprint]; ok || len(m.fingerprints) < 256 {
				v := m.fingerprints[q.fingerprint]
				v.Calls++
				v.MS += float64(elapsed.Nanoseconds()) / 1e6
				m.fingerprints[q.fingerprint] = v
			}
		}
	}
}
func (Tracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	if ctx.Value(metricsKey{}) != nil {
		return context.WithValue(ctx, acquireStartKey{}, time.Now())
	}
	return ctx
}
func (Tracer) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	if start, ok := ctx.Value(acquireStartKey{}).(time.Time); ok {
		ctx.Value(metricsKey{}).(*Metrics).AcquireNanos.Add(time.Since(start).Nanoseconds())
	}
}
