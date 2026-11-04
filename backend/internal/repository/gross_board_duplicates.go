package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type grossBoardLoadClaim struct {
	DriverID     string `json:"driverId"`
	Date         string `json:"date"`
	Slot         int    `json:"slot"`
	LoadNumber   string `json:"loadNumber"`
	LoadRecordID *int   `json:"loadRecordId"`
	LoadTripID   *int   `json:"loadTripId"`
}

// Existing duplicates remain editable for reviewed cleanup. Only acquiring a
// new number/trip checks occupancy; editing amounts does not scan the board.
func newGrossBoardLoadClaim(e GrossBoardEntry, oldNumber string, oldID, oldTrip *int, oldActive bool) bool {
	if e.Deleted || e.DayStatus != "" || strings.TrimSpace(e.LoadNumber) == "" {
		return false
	}
	if !oldActive || !strings.EqualFold(strings.TrimSpace(e.LoadNumber), strings.TrimSpace(oldNumber)) {
		return true
	}
	if e.LoadTripID == nil || oldTrip == nil {
		return e.LoadTripID != nil || oldTrip != nil
	}
	return *e.LoadTripID != *oldTrip || e.LoadRecordID == nil || oldID == nil || *e.LoadRecordID != *oldID
}

// Check the final batch state, so clearing/moving a load atomically is allowed.
// The caller holds the native financial/source lock throughout the writes and
// this check, including across concurrent dispatchers. No financial calculation
// is needed. Different reviewed trips of the same parent are separate claims.
func assertGrossBoardLoadClaims(ctx context.Context, tx pgx.Tx, claims []grossBoardLoadClaim) error {
	if len(claims) == 0 {
		return nil
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	var number, driver, date string
	var slot int
	// Decode the compact batch once and expose its actual size to the planner;
	// the default JSON row estimate otherwise favors repeated scans/parallelism.
	err = tx.QueryRow(ctx, `WITH claims AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset($1::jsonb) AS c("driverId" uuid,date date,slot integer,"loadNumber" text,"loadRecordId" integer,"loadTripId" integer)
 LIMIT $2
 ) SELECT c."loadNumber",d.full_name,b.service_date::text,b.slot
 FROM claims c JOIN `+grossBoardEntriesSQL+` b
 ON (lower(btrim(b.load_number))=lower(btrim(c."loadNumber")) OR b.load_record_id=c."loadRecordId")
 JOIN drivers d ON d.id=b.driver_id
 WHERE NOT b.deleted AND b.day_status='' AND btrim(b.load_number)<>''
 AND (b.driver_id,b.service_date,b.slot)<>(c."driverId",c.date,c.slot)
 AND NOT (b.load_trip_id IS NOT NULL AND c."loadTripId" IS NOT NULL
   AND b.load_record_id IS NOT NULL AND c."loadRecordId" IS NOT NULL
   AND b.load_record_id=c."loadRecordId" AND b.load_trip_id<>c."loadTripId")
 ORDER BY c.date,c.slot,b.service_date,b.slot,d.full_name LIMIT 1`, string(body), len(claims)).Scan(&number, &driver, &date, &slot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return chargeInvalid("Load %s is already on Gross Board for %s on %s (load %d). Edit or move the existing entry instead of adding it again.", number, driver, date, slot+1)
}
