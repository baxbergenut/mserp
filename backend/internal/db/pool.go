package db

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	// Short ERP queries do not amortize LLVM compilation on the shared VPS.
	config.ConnConfig.RuntimeParams["jit"] = "off"
	config.ConnConfig.Tracer = &poolTracer{}
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = time.Minute

	return pgxpool.NewWithConfig(ctx, config)
}

// pgx supplies the request context at acquisition and the connection at release.
type poolTracer struct {
	Tracer
	holders sync.Map // *pgx.Conn -> poolHold
}

type poolHold struct {
	metrics *Metrics
	since   time.Time
}

func (p *poolTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	p.Tracer.TraceAcquireEnd(ctx, pool, data)
	if m, ok := ctx.Value(metricsKey{}).(*Metrics); ok && data.Err == nil && data.Conn != nil {
		m.Acquires.Add(1)
		p.holders.Store(data.Conn, poolHold{m, time.Now()})
	}
}

func (p *poolTracer) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) {
	if value, ok := p.holders.LoadAndDelete(data.Conn); ok {
		hold := value.(poolHold)
		hold.metrics.HoldNanos.Add(time.Since(hold.since).Nanoseconds())
	}
}
