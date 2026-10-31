package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestBatchMetricsCountStatements(t *testing.T) {
	ctx, m := WithQueryMetrics(context.Background())
	ctx = Tracer{}.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{})
	for range 2 {
		Tracer{}.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{SQL: "SELECT private_financial_column WHERE secret=$1", Args: []any{"private-financial-value"}})
	}
	Tracer{}.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})
	_, queries := m.Details()
	if m.Queries.Load() != 2 || len(queries) != 1 {
		t.Fatalf("batch statements disappeared from request metrics: %d %d", m.Queries.Load(), len(queries))
	}
	for key, q := range queries {
		if len(key) != 16 || q.Calls != 2 {
			t.Fatal("batch metrics must contain only bounded fingerprints and counts")
		}
	}
}

func TestQueryDiagnosticsAreBoundedAndExcludeValues(t *testing.T) {
	ctx, m := WithQueryMetrics(context.Background())
	for i := 0; i < 300; i++ {
		q := Tracer{}.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: fmt.Sprintf("SELECT private_column_%d WHERE secret=$1", i), Args: []any{"private-financial-value"}})
		Tracer{}.TraceQueryEnd(q, nil, pgx.TraceQueryEndData{})
	}
	_, queries := m.Details()
	if len(queries) != 256 || m.Queries.Load() != 300 {
		t.Fatal("diagnostic bound lost")
	}
	body, _ := json.Marshal(queries)
	if strings.Contains(string(body), "private") || strings.Contains(string(body), "SELECT") || strings.Contains(string(body), "secret") {
		t.Fatal("query content leaked")
	}
	ctx, m = WithMetrics(context.Background())
	q := Tracer{}.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	Tracer{}.TraceQueryEnd(q, nil, pgx.TraceQueryEndData{})
	_, queries = m.Details()
	if len(queries) != 0 {
		t.Fatal("ordinary request opted into query fingerprints")
	}
}
