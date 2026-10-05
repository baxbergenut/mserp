package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FiveELDLocation struct {
	VIN                 string
	ProviderTruckNumber string
	Address             string
	Latitude            float64
	Longitude           float64
	ReportedAt          time.Time
	FetchedAt           time.Time
	AddressUpdatedAt    *time.Time
	AddressReportedAt   *time.Time
}

type FiveELDSyncResult struct {
	Fetched        int       `json:"fetched"`
	Saved          int       `json:"saved"`
	Unmatched      int       `json:"unmatched"`
	Ambiguous      int       `json:"ambiguous"`
	Invalid        int       `json:"invalid"`
	AddressLookups int       `json:"addressLookups"`
	SyncedAt       time.Time `json:"syncedAt"`
}

type FiveELDRepository struct{ pool *pgxpool.Pool }

func NewFiveELDRepository(pool *pgxpool.Pool) *FiveELDRepository {
	return &FiveELDRepository{pool: pool}
}

func (r *FiveELDRepository) ActiveTruckVINs(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g'))
 FROM drivers d
 JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 JOIN trucks t ON t.id=a.truck_id
 WHERE d.active AND t.active AND nullif(btrim(t.vin),'') IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]struct{}{}
	for rows.Next() {
		var vin string
		if err = rows.Scan(&vin); err != nil {
			return nil, err
		}
		result[vin] = struct{}{}
	}
	return result, rows.Err()
}

func (r *FiveELDRepository) Locations(ctx context.Context, vins []string) (map[string]FiveELDLocation, error) {
	result := map[string]FiveELDLocation{}
	if len(vins) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT vin,provider_truck_number,address,latitude,longitude,reported_at,fetched_at,address_updated_at,address_reported_at
 FROM five_eld_locations WHERE vin=ANY($1)`, vins)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value FiveELDLocation
		if err = rows.Scan(&value.VIN, &value.ProviderTruckNumber, &value.Address, &value.Latitude, &value.Longitude,
			&value.ReportedAt, &value.FetchedAt, &value.AddressUpdatedAt, &value.AddressReportedAt); err != nil {
			return nil, err
		}
		result[value.VIN] = value
	}
	return result, rows.Err()
}

func (r *FiveELDRepository) StoreLocations(ctx context.Context, locations []FiveELDLocation, unmatched, ambiguous []string, invalid int, staleAfter time.Duration, result FiveELDSyncResult) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, value := range locations {
		_, err = tx.Exec(ctx, `INSERT INTO five_eld_locations
 (vin,provider_truck_number,address,latitude,longitude,reported_at,fetched_at,address_updated_at,address_reported_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 ON CONFLICT(vin) DO UPDATE SET
 provider_truck_number=CASE WHEN EXCLUDED.reported_at>=five_eld_locations.reported_at THEN EXCLUDED.provider_truck_number ELSE five_eld_locations.provider_truck_number END,
 latitude=CASE WHEN EXCLUDED.reported_at>=five_eld_locations.reported_at THEN EXCLUDED.latitude ELSE five_eld_locations.latitude END,
 longitude=CASE WHEN EXCLUDED.reported_at>=five_eld_locations.reported_at THEN EXCLUDED.longitude ELSE five_eld_locations.longitude END,
 reported_at=greatest(EXCLUDED.reported_at,five_eld_locations.reported_at),
 fetched_at=greatest(EXCLUDED.fetched_at,five_eld_locations.fetched_at),
 address=CASE WHEN EXCLUDED.address<>'' THEN EXCLUDED.address ELSE five_eld_locations.address END,
	address_updated_at=coalesce(EXCLUDED.address_updated_at,five_eld_locations.address_updated_at),
	address_reported_at=coalesce(EXCLUDED.address_reported_at,five_eld_locations.address_reported_at)`,
			value.VIN, value.ProviderTruckNumber, value.Address, value.Latitude, value.Longitude,
			value.ReportedAt, value.FetchedAt, value.AddressUpdatedAt, value.AddressReportedAt)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO five_eld_sync_state
 (singleton,last_attempt_at,last_success_at,last_error,stale_after_seconds,unmatched_vins,ambiguous_vins,invalid_unit_count)
 VALUES(true,$1,$1,'',$2,$3,$4,$5)
 ON CONFLICT(singleton) DO UPDATE SET last_attempt_at=EXCLUDED.last_attempt_at,last_success_at=EXCLUDED.last_success_at,
 last_error='',stale_after_seconds=EXCLUDED.stale_after_seconds,unmatched_vins=EXCLUDED.unmatched_vins,
 ambiguous_vins=EXCLUDED.ambiguous_vins,invalid_unit_count=EXCLUDED.invalid_unit_count`,
		result.SyncedAt, int(staleAfter.Seconds()), unmatched, ambiguous, invalid)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *FiveELDRepository) RecordFailure(ctx context.Context, attemptedAt time.Time, staleAfter time.Duration, syncErr error) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO five_eld_sync_state(singleton,last_attempt_at,last_error,stale_after_seconds)
 VALUES(true,$1,$2,$3) ON CONFLICT(singleton) DO UPDATE SET
 last_attempt_at=EXCLUDED.last_attempt_at,last_error=EXCLUDED.last_error,stale_after_seconds=EXCLUDED.stale_after_seconds`,
		attemptedAt, syncErr.Error(), int(staleAfter.Seconds()))
	return err
}

func fiveELDSyncSummary(ctx context.Context, tx pgx.Tx) (FiveELDBoardSummary, error) {
	var value FiveELDBoardSummary
	err := tx.QueryRow(ctx, `SELECT last_attempt_at,last_success_at,last_error,cardinality(unmatched_vins),cardinality(ambiguous_vins),invalid_unit_count
 FROM five_eld_sync_state WHERE singleton`).Scan(&value.LastAttemptAt, &value.LastSuccessAt, &value.LastError,
		&value.Unmatched, &value.Ambiguous, &value.Invalid)
	if err == pgx.ErrNoRows {
		return value, nil
	}
	return value, err
}
