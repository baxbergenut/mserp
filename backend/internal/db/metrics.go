package db

import (
	"context"
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
}
type metricsKey struct{}
type queryStartKey struct{}
type acquireStartKey struct{}

func WithMetrics(ctx context.Context) (context.Context, *Metrics) {
	m := &Metrics{}
	return context.WithValue(ctx, metricsKey{}, m), m
}

type Tracer struct{}

func (Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if m, ok := ctx.Value(metricsKey{}).(*Metrics); ok {
		m.Queries.Add(1)
		return context.WithValue(ctx, queryStartKey{}, time.Now())
	}
	return ctx
}
func (Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if start, ok := ctx.Value(queryStartKey{}).(time.Time); ok {
		ctx.Value(metricsKey{}).(*Metrics).QueryNanos.Add(time.Since(start).Nanoseconds())
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
