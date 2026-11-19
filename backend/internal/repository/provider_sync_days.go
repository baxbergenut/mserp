package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type syncDayWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func recordSyncDay(ctx context.Context, writer syncDayWriter, provider, environment, day string, count int, fetchedAt *time.Time) error {
	_, err := writer.Exec(ctx, `INSERT INTO provider_sync_days(provider,environment,sync_date,transaction_count,fetched_at)
 VALUES ($1,$2,$3::date,$4,coalesce($5::timestamptz,now())) ON CONFLICT(provider,environment,sync_date)
 DO UPDATE SET transaction_count=EXCLUDED.transaction_count,fetched_at=EXCLUDED.fetched_at`, provider, environment, day, count, fetchedAt)
	return err
}
