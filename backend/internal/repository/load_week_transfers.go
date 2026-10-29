package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Both projections retain source coordinates for historical attribution and
// mutations. Only the displayed/accounted date and dispatch slot differ.
func boardPlacementSQL(payroll bool) string {
	date, slot := "dispatch_date", "coalesce(t.dispatch_slot,e.slot)"
	if payroll {
		date, slot = "settlement_date", "CASE WHEN t.settlement_date<>e.service_date THEN t.dispatch_slot ELSE e.slot END"
	}
	return `(SELECT e.driver_id,coalesce(t.` + date + `,e.service_date) service_date,` + slot + ` slot,e.deleted,
 e.load_number,e.load_record_id,e.original_rate,e.driver_rate,e.miles,e.version,e.entered_original_rate,e.entered_miles,e.day_status,e.note,e.load_trip_id,
 e.service_date source_date,e.slot source_slot,coalesce(t.version,0) transfer_version,
 coalesce(t.dispatch_date::text,'') dispatch_date,coalesce(t.dispatch_slot,e.slot) dispatch_slot,
 coalesce(t.dispatcher_id,'') credited_dispatcher_id,coalesce(t.dispatcher_name,'') credited_dispatcher_name,t.truck_id transfer_truck_id
 FROM ` + grossBoardEntriesSQL + ` e LEFT JOIN gross_board_transfers t ON t.driver_id=e.driver_id AND t.source_date=e.service_date AND t.source_slot=e.slot AND t.active
 WHERE NOT ((e.deleted OR (btrim(e.load_number)='' AND e.day_status='' AND e.original_rate IS NULL AND e.driver_rate IS NULL AND e.miles IS NULL))
 AND EXISTS(SELECT 1 FROM gross_board_transfers reserved WHERE reserved.active AND reserved.driver_id=e.driver_id AND reserved.dispatch_date=e.service_date AND reserved.dispatch_slot=e.slot)))`
}

var dispatchBoardEntriesSQL = boardPlacementSQL(false)
var payrollBoardEntriesSQL = boardPlacementSQL(true)

type LoadWeekTransferInput struct {
	DriverID        string `json:"driverId"`
	Date            string `json:"date"`
	Slot            int    `json:"slot"`
	BoardVersion    int    `json:"boardVersion"`
	TransferVersion int    `json:"transferVersion"`
	Mode            string `json:"mode"` // everyone, dispatch_only, restore
	Reason          string `json:"reason"`
}

func (r *DriverPayRepository) TransferLoadWeek(ctx context.Context, input LoadWeekTransferInput, actor string) error {
	date, err := time.Parse(time.DateOnly, input.Date)
	if err != nil || date.Year() < 2000 || date.Year() > 2100 || (date.Weekday() != time.Saturday && date.Weekday() != time.Sunday) || input.Slot < 0 || input.Slot > 99 || input.BoardVersion < 1 || input.TransferVersion < 0 {
		return chargeInvalid("Select a Saturday or Sunday load")
	}
	if input.Mode != "everyone" && input.Mode != "dispatch_only" && input.Mode != "restore" {
		return chargeInvalid("Invalid transfer option")
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len([]rune(input.Reason)) > 1000 {
		return chargeInvalid("Provide a reason, up to 1,000 characters")
	}
	week := date.AddDate(0, 0, -(int(date.Weekday())+6)%7)
	next := week.AddDate(0, 0, 7).Format(time.DateOnly)
	settlement := input.Date
	if input.Mode == "everyone" {
		settlement = next
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	if err = lockChargeDrivers(ctx, tx, []string{input.DriverID}); err != nil {
		return err
	}
	var boardVersion int
	var number string
	var loadID *int
	var tripReview bool
	err = tx.QueryRow(ctx, `SELECT e.version,e.load_number,l.id,coalesce(trip_source.review,false) FROM `+grossBoardEntriesSQL+` e `+grossBoardResolvedLoad+loadTripSourceSQL+` WHERE e.driver_id=$1 AND e.service_date=$2::date AND e.slot=$3 AND NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''`, input.DriverID, input.Date, input.Slot).Scan(&boardVersion, &number, &loadID, &tripReview)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDriverPayConflict
	}
	if err != nil {
		return err
	}
	if boardVersion != input.BoardVersion {
		return ErrDriverPayConflict
	}
	if loadID == nil || tripReview {
		return chargeInvalid("Resolve the load and its trip in Gross Board before transferring")
	}
	var before json.RawMessage
	var oldDate, dispatcherID, dispatcherName string
	var truck *string
	var slot, version int
	var active bool
	err = tx.QueryRow(ctx, `SELECT to_jsonb(t),settlement_date::text,dispatch_slot,version,active,dispatcher_id,dispatcher_name,truck_id::text FROM gross_board_transfers t WHERE driver_id=$1 AND source_date=$2::date AND source_slot=$3 FOR UPDATE`, input.DriverID, input.Date, input.Slot).Scan(&before, &oldDate, &slot, &version, &active, &dispatcherID, &dispatcherName, &truck)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if !active {
		oldDate = input.Date
	}
	expected := version
	if input.TransferVersion != expected {
		return ErrDriverPayConflict
	}
	if input.Mode == "restore" && !active {
		return ErrDriverPayConflict
	}
	if !active {
		assignments, e := readDailyAssignments(ctx, tx, week.Format(time.DateOnly), []string{input.DriverID})
		if e != nil {
			return e
		}
		found := false
		for _, a := range assignments[input.DriverID] {
			if a.Date == input.Date {
				dispatcherID, dispatcherName = a.DispatcherID, a.DispatcherName
				found = true
			}
		}
		if !found {
			return chargeInvalid("Review the original dispatcher assignment before transferring")
		}
		matches, e := truckPayLoadMap(ctx, tx, week.Format(time.DateOnly))
		if e != nil {
			return e
		}
		if id := matches[input.DriverID+":"+payCommentKey(input.Date, input.Slot, number)].TruckID; id != "" {
			truck = &id
		}
		err = tx.QueryRow(ctx, `SELECT n FROM generate_series(0,99) n WHERE NOT EXISTS(SELECT 1 FROM `+grossBoardEntriesSQL+` e WHERE e.driver_id=$1 AND e.service_date=$2::date AND e.slot=n AND NOT e.deleted AND (btrim(e.load_number)<>'' OR e.day_status<>'' OR e.original_rate IS NOT NULL OR e.driver_rate IS NOT NULL OR e.miles IS NOT NULL)) AND NOT EXISTS(SELECT 1 FROM gross_board_transfers t WHERE t.driver_id=$1 AND t.dispatch_date=$2::date AND t.dispatch_slot=n AND t.active) ORDER BY n LIMIT 1`, input.DriverID, next).Scan(&slot)
		if errors.Is(err, pgx.ErrNoRows) {
			return chargeInvalid("Next Monday already has 100 loads")
		}
		if err != nil {
			return err
		}
	}
	if oldDate != settlement {
		if truck == nil {
			return chargeInvalid("Resolve the original truck before moving the settlement")
		}
		if err = assertTransferSettlementsOpen(ctx, tx, input.DriverID, truck, week.Format(time.DateOnly), next); err != nil {
			return err
		}
		// A changed crew or investor agreement cannot silently acquire this load.
		var safe bool
		err = tx.QueryRow(ctx, `SELECT payroll_driver_at($1::uuid,$2::date)=payroll_driver_at($1::uuid,$3::date) AND
  (SELECT coalesce(jsonb_agg(jsonb_build_array(owner_id,share_percent)), '[]') FROM (SELECT owner_id,share_percent FROM truck_settlement_terms WHERE truck_id=$4::uuid AND week_start<=$2::date ORDER BY week_start DESC LIMIT 1) x)
  IS NOT DISTINCT FROM (SELECT coalesce(jsonb_agg(jsonb_build_array(owner_id,share_percent)), '[]') FROM (SELECT owner_id,share_percent FROM truck_settlement_terms WHERE truck_id=$4::uuid AND week_start<=$3::date ORDER BY week_start DESC LIMIT 1) x)`, input.DriverID, week.Format(time.DateOnly), next, truck).Scan(&safe)
		if err != nil {
			return err
		}
		if !safe {
			return chargeInvalid("Crew or investor terms change between these weeks; review them before moving the settlement")
		}
	}
	var after json.RawMessage
	err = tx.QueryRow(ctx, `INSERT INTO gross_board_transfers(driver_id,source_date,source_slot,dispatch_date,dispatch_slot,settlement_date,dispatcher_id,dispatcher_name,truck_id,active)
 VALUES($1,$2::date,$3,$4::date,$5,$6::date,$7,$8,$9,$10)
 ON CONFLICT(driver_id,source_date,source_slot) DO UPDATE SET settlement_date=excluded.settlement_date,active=excluded.active,dispatch_slot=excluded.dispatch_slot,version=gross_board_transfers.version+1
 RETURNING to_jsonb(gross_board_transfers)`, input.DriverID, input.Date, input.Slot, next, slot, settlement, dispatcherID, dispatcherName, truck, input.Mode != "restore").Scan(&after)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO gross_board_transfer_history(actor_id,reason,before_value,after_value) VALUES($1,$2,$3,$4)`, actor, input.Reason, before, after); err != nil {
		return err
	}
	// Transfer metadata does not update the source board row, so announce its
	// placement change explicitly. PostgreSQL delivers this only after commit.
	if _, err = tx.Exec(ctx, `SELECT pg_notify('mserp_gross_board','')`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func assertTransferSettlementsOpen(ctx context.Context, tx pgx.Tx, driver string, truck *string, weeks ...string) error {
	for _, week := range weeks {
		var closed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_settlements WHERE finalized AND week_start=$2::date AND (driver_id=$1 OR driver_id=payroll_driver_at($1::uuid,$2::date))) OR EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE finalized AND week_start=$2::date AND (truck_id=$3::uuid OR EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(w.report->'loads','[]')) l WHERE l->>'sourceDriverId'=$1::text)))`, driver, week, truck).Scan(&closed)
		if err != nil {
			return err
		}
		if closed {
			return chargeInvalid("Reopen affected driver and investor statements in both weeks before moving the settlement")
		}
	}
	return nil
}

// Placement annotations are live metadata even on frozen statements. Stored
// monetary snapshots and their dates/amounts are never rewritten by this read.
func decorateTransferLoads(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	rows, err := tx.Query(ctx, `SELECT t.driver_id::text,t.source_date::text,t.source_slot,t.dispatch_date::text,t.dispatch_slot,t.version,t.active,t.dispatcher_id,t.dispatcher_name,e.version FROM gross_board_transfers t JOIN `+grossBoardEntriesSQL+` e ON e.driver_id=t.driver_id AND e.service_date=t.source_date AND e.slot=t.source_slot WHERE (t.source_date >= $1::date AND t.source_date < $1::date+7) OR t.dispatch_date=$1::date`, report.WeekStart)
	if err != nil {
		return err
	}
	type annotation struct {
		source, date, id, name string
		slot, version, board   int
		active                 bool
	}
	values := map[string]annotation{}
	for rows.Next() {
		var a annotation
		var driver string
		var sourceSlot int
		if err = rows.Scan(&driver, &a.source, &sourceSlot, &a.date, &a.slot, &a.version, &a.active, &a.id, &a.name, &a.board); err != nil {
			rows.Close()
			return err
		}
		values[driver+":"+payCommentKey(a.source, sourceSlot, "")] = a
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		for j := range d.Loads {
			l := &d.Loads[j]
			driver := l.SourceDriverID
			if driver == "" {
				driver = d.ID
			}
			source := l.SourceDate
			if source == "" {
				source = l.Date
			}
			sourceSlot := l.Slot
			if l.SourceDate != "" {
				sourceSlot = l.SourceSlot
			}
			if a, ok := values[driver+":"+payCommentKey(source, sourceSlot, "")]; ok {
				l.SourceSlot = sourceSlot
				l.SourceDate = a.source
				l.BoardVersion = a.board
				l.TransferVersion = 0
				l.PlacementVersion = a.version
				l.DispatchDate = ""
				l.DispatchSlot = 0
				l.CreditedDispatcherID = ""
				l.CreditedDispatcherName = ""
				if a.active {
					l.TransferVersion = a.version
					l.DispatchDate = a.date
					l.DispatchSlot = a.slot
					l.CreditedDispatcherID = a.id
					l.CreditedDispatcherName = a.name
				}
			}
		}
	}
	return nil
}
