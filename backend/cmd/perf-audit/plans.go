package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

type planTraceKey struct{}
type planQuery struct {
	sql     string
	args    []any
	start   time.Time
	elapsed time.Duration
}
type planTracer struct {
	db.Tracer
	mu   sync.Mutex
	slow planQuery
}

func (p *planTracer) TraceQueryStart(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	ctx = p.Tracer.TraceQueryStart(ctx, c, d)
	return context.WithValue(ctx, planTraceKey{}, planQuery{sql: d.SQL, args: d.Args, start: time.Now()})
}
func (p *planTracer) TraceQueryEnd(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryEndData) {
	p.Tracer.TraceQueryEnd(ctx, c, d)
	q := ctx.Value(planTraceKey{}).(planQuery)
	q.elapsed = time.Since(q.start)
	if !strings.HasPrefix(q.sql, "SELECT") && !strings.HasPrefix(q.sql, "WITH") {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if q.elapsed > p.slow.elapsed {
		p.slow = q
	}
}

// Explicit diagnostics only. Bound the repeated read and strip every expression,
// literal value from EXPLAIN before printing operational counters.
func (p *planTracer) explain(ctx context.Context, pool *pgxpool.Pool) (any, error) {
	p.mu.Lock()
	q := p.slow
	p.mu.Unlock()
	if q.sql == "" {
		return nil, nil
	}
	tx, e := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SET LOCAL statement_timeout='3s'; SET LOCAL plan_cache_mode=force_custom_plan`); e != nil {
		return nil, e
	}
	var raw []byte
	if e = tx.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+q.sql, q.args...).Scan(&raw); e != nil {
		return nil, e
	}
	var plans []map[string]any
	if e = json.Unmarshal(raw, &plans); e != nil {
		return nil, e
	}
	var clean func(map[string]any) map[string]any
	clean = func(node map[string]any) map[string]any {
		out := map[string]any{}
		for _, key := range []string{"Node Type", "Relation Name", "Index Name", "Join Type", "Strategy", "Plan Rows", "Actual Rows", "Actual Loops", "Actual Total Time", "Rows Removed by Filter", "Rows Removed by Join Filter", "Shared Hit Blocks", "Shared Read Blocks", "Temp Read Blocks", "Temp Written Blocks", "Planning Time", "Execution Time"} {
			if v, ok := node[key]; ok {
				out[key] = v
			}
		}
		if v, ok := node["Plan"].(map[string]any); ok {
			out["Plan"] = clean(v)
		}
		if children, ok := node["Plans"].([]any); ok {
			v := []any{}
			for _, child := range children {
				v = append(v, clean(child.(map[string]any)))
			}
			out["Plans"] = v
		}
		return out
	}
	return clean(plans[0]), nil
}
