package repository

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
)

// Synthetic, disposable-database diagnostics only. No query arguments are
// recorded; production performance logs continue to contain counts/hashes only.
type payrollTestQueriesKey struct{}
type payrollTestQueries struct {
	mu  sync.Mutex
	SQL []string
}
type payrollTestTracer struct{ db.Tracer }

func recordPayrollTestQuery(ctx context.Context, sql string) {
	if trace, ok := ctx.Value(payrollTestQueriesKey{}).(*payrollTestQueries); ok {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		trace.SQL = append(trace.SQL, sql)
	}
}
func (t payrollTestTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	recordPayrollTestQuery(ctx, data.SQL)
	return t.Tracer.TraceQueryStart(ctx, conn, data)
}
func (t payrollTestTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	recordPayrollTestQuery(ctx, data.SQL)
	t.Tracer.TraceBatchQuery(ctx, conn, data)
}
