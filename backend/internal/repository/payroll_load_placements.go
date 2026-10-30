package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The dispatch projection is never changed. An explicit payroll decision replaces
// all automatic occurrences of that exact imported load/trip allocation.
func payrollPlacementSQL() string {
	return `(SELECT e.*,false payroll_supplement,0 payroll_placement_version FROM ` + boardPlacementSQL(true) + ` e
 WHERE NOT EXISTS(SELECT 1 FROM payroll_load_placements p WHERE (p.load_record_id=coalesce(e.load_record_id,
 (SELECT min(l.id) FROM loads l WHERE lower(btrim(l.load_id))=lower(btrim(e.load_number)) HAVING count(*)=1))
 OR (e.load_record_id IS NULL AND p.driver_id=e.driver_id AND p.source_date=e.source_date AND p.source_slot=e.source_slot
 AND EXISTS(SELECT 1 FROM loads recognized WHERE recognized.id=p.load_record_id AND lower(btrim(recognized.load_id))=lower(btrim(e.load_number)))))
 AND (p.load_trip_id=coalesce(e.load_trip_id,0) OR (p.included AND (p.load_trip_id=0 OR e.load_trip_id IS NULL)))
 AND (p.included OR p.week_start=date_trunc('week',e.service_date)::date
 OR EXISTS(SELECT 1 FROM payroll_load_placement_history h WHERE h.action='remove'
 AND h.after_value->>'load_record_id'=p.load_record_id::text
 AND h.after_value->>'load_trip_id'=p.load_trip_id::text
 AND h.after_value->>'week_start'=date_trunc('week',e.service_date)::date::text)))
 UNION ALL SELECT p.driver_id,p.week_start+6,p.slot,false,l.load_id,l.id,p.original_rate,p.driver_gross,p.miles,0,
 p.original_rate,p.miles,''::text,''::text,nullif(p.load_trip_id,0),p.source_date,p.source_slot,0,
 ''::text,p.slot,''::text,''::text,p.truck_id,true,p.version
 FROM payroll_load_placements p JOIN loads l ON l.id=p.load_record_id WHERE p.included)`
}

func payLoadReady(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "delivered", "invoiced":
		return true
	}
	return false
}

type PayLoadPlacementInput struct {
	DriverID         string `json:"driverId"` // statement ID (truck ID for Investor Pay)
	WeekStart        string `json:"weekStart"`
	Action           string `json:"action"`
	LoadNumber       string `json:"loadNumber"`
	LoadRecordID     int    `json:"loadRecordId"`
	LoadTripID       *int   `json:"loadTripId"`
	SourceDriverID   string `json:"sourceDriverId"`
	CommentKey       string `json:"commentKey"`
	BoardVersion     int    `json:"boardVersion"`
	PlacementVersion int    `json:"placementVersion"`
	DriverGross      string `json:"driverGross"`
}

type PayLoadPlacementChoice struct {
	SourceDriverID string `json:"sourceDriverId"`
	DriverName     string `json:"driverName"`
	LoadTripID     *int   `json:"loadTripId,omitempty"`
	TripNumber     string `json:"tripNumber"`
	SourceDate     string `json:"sourceDate"`
}
type PayLoadChoiceRequired struct {
	Choices []PayLoadPlacementChoice `json:"choices"`
}

func (e *PayLoadChoiceRequired) Error() string { return "Select the load's native trip allocation" }

// Mutations serialize with finalization before taking a READ COMMITTED snapshot.
// Revenue and hired labor stay together in both payroll views.
func (r *DriverPayRepository) PlacePayLoad(ctx context.Context, kind string, in PayLoadPlacementInput, actor string) error {
	week, err := time.Parse(time.DateOnly, in.WeekStart)
	if err != nil || week.Weekday() != time.Monday || week.Year() < 2000 || week.Year() > 2100 || (in.Action != "add" && in.Action != "remove" && in.Action != "gross") {
		return chargeInvalid("Select a valid statement week and load action")
	}
	in.LoadNumber = strings.TrimSpace(in.LoadNumber)
	if in.Action == "add" && (in.LoadNumber == "" || len([]rune(in.LoadNumber)) > 200) {
		return chargeInvalid("Enter a load number")
	}
	if in.LoadTripID != nil && *in.LoadTripID < 1 {
		return chargeInvalid("Invalid load trip")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	var report DriverPayWeek
	if kind == "investor" {
		report, err = readInvestorPay(ctx, tx, week)
	} else {
		report, err = readDriverPayWeek(ctx, tx, week, in.DriverID)
	}
	if err != nil {
		return err
	}
	var card *DriverPayDriver
	for i := range report.Drivers {
		if report.Drivers[i].ID == in.DriverID {
			card = &report.Drivers[i]
			break
		}
	}
	if card == nil {
		return chargeInvalid("Statement no longer exists; reload this week")
	}
	if card.Settlement != nil && card.Settlement.Finalized {
		return chargeInvalid("Reopen this statement before changing its loads")
	}
	var load DriverPayLoad
	driver, truck, sourceDate, sourceSlot := in.DriverID, "", "", 0
	var before json.RawMessage
	var oldWeek, oldDriver string
	var oldTruck *string
	var oldVersion int
	var oldIncluded bool
	if in.Action != "add" {
		found := false
		for _, l := range card.Loads {
			if l.CommentKey == in.CommentKey && l.LoadRecordID != nil && *l.LoadRecordID == in.LoadRecordID {
				load = l
				found = true
				break
			}
		}
		if !found || load.BoardVersion != in.BoardVersion || load.PayrollPlacementVersion != in.PlacementVersion {
			return ErrDriverPayConflict
		}
		if in.Action == "remove" && !load.PayrollSupplement && !load.NotReady {
			return chargeInvalid("Only flagged or manually added loads can be removed from payroll")
		}
		if in.Action == "gross" && !load.PayrollSupplement {
			return chargeInvalid("Edit this load's driver gross in Gross Board")
		}
		driver = load.SourceDriverID
		if driver == "" {
			driver = card.ID
		}
		sourceDate, sourceSlot = load.SourceDate, load.SourceSlot
		if sourceDate == "" {
			sourceDate, sourceSlot = load.Date, load.Slot
		}
	} else {
		rows, e := tx.Query(ctx, `SELECT id FROM loads WHERE lower(btrim(load_id))=lower(btrim($1)) ORDER BY id`, in.LoadNumber)
		if e != nil {
			return e
		}
		ids := []int{}
		for rows.Next() {
			var id int
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(ids) == 0 {
			return chargeInvalid("Load not found in the system")
		}
		if len(ids) > 1 {
			return chargeInvalid("This load number matches multiple source records; resolve its identity in Gross Board")
		}
		in.LoadRecordID = ids[0]
		var status string
		if err = tx.QueryRow(ctx, `SELECT status,coalesce((coalesce(delivery_time,delivery_appointment_time,pickup_time,pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,'') FROM loads WHERE id=$1`, in.LoadRecordID).Scan(&status, &sourceDate); err != nil {
			return err
		}
		if !payLoadReady(status) {
			return chargeInvalid("Only delivered or invoiced loads can be added (current status: %s)", status)
		}
		// Native board allocations supply reviewed driver gross and split selection.
		rows, e = tx.Query(ctx, `SELECT e.driver_id::text,e.source_date::text,e.source_slot,e.load_trip_id,coalesce(e.driver_rate::text,''),coalesce(e.entered_original_rate::text,''),coalesce(e.entered_miles::text,''),d.full_name,
 coalesce((SELECT v->>'trip_id' FROM jsonb_array_elements(CASE WHEN jsonb_typeof(l.raw_payload->'trips')='array' THEN l.raw_payload->'trips' ELSE '[]'::jsonb END) v WHERE v->>'id'=e.load_trip_id::text LIMIT 1),''),coalesce(trip_source.review,false)
 FROM `+boardPlacementSQL(true)+` e JOIN drivers d ON d.id=e.driver_id JOIN loads l ON l.id=$1 `+loadTripSourceSQL+` WHERE NOT e.deleted AND e.day_status='' AND
 (e.load_record_id=$1 OR (e.load_record_id IS NULL AND lower(btrim(e.load_number))=lower(btrim($2))))
 ORDER BY e.source_date,e.source_slot`, in.LoadRecordID, in.LoadNumber)
		if e != nil {
			return e
		}
		type allocation struct {
			driver, date, gross, original, miles, name, tripNumber string
			slot                                                   int
			trip                                                   *int
			review                                                 bool
		}
		allocations := []allocation{}
		seen := map[string]bool{}
		for rows.Next() {
			var a allocation
			if e = rows.Scan(&a.driver, &a.date, &a.slot, &a.trip, &a.gross, &a.original, &a.miles, &a.name, &a.tripNumber, &a.review); e != nil {
				rows.Close()
				return e
			}
			key := a.driver + ":" + loadAllocationKey(in.LoadRecordID, a.trip)
			if !seen[key] {
				allocations = append(allocations, a)
				seen[key] = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		matches := []allocation{}
		for _, a := range allocations {
			var routed string
			if err = tx.QueryRow(ctx, `SELECT payroll_driver_at($1::uuid,$2::date)::text`, a.driver, in.WeekStart).Scan(&routed); err != nil {
				return err
			}
			if kind != "investor" && routed != in.DriverID {
				continue
			}
			if in.SourceDriverID != "" && a.driver != in.SourceDriverID {
				continue
			}
			if in.LoadTripID != nil && (a.trip == nil || *a.trip != *in.LoadTripID) {
				continue
			}
			if kind == "investor" {
				var assigned bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_driver_assignments WHERE driver_id=$1 AND truck_id=$2 AND assigned_at<(($3::date+1)::timestamp AT TIME ZONE 'America/New_York') AND (unassigned_at IS NULL OR unassigned_at>=(($3::date+1)::timestamp AT TIME ZONE 'America/New_York')))`, a.driver, in.DriverID, a.date).Scan(&assigned); err != nil {
					return err
				}
				if !assigned {
					continue
				}
			}
			matches = append(matches, a)
		}
		if len(matches) > 1 {
			choices := []PayLoadPlacementChoice{}
			for _, a := range matches {
				choices = append(choices, PayLoadPlacementChoice{SourceDriverID: a.driver, DriverName: a.name, LoadTripID: a.trip, TripNumber: a.tripNumber, SourceDate: a.date})
			}
			return &PayLoadChoiceRequired{Choices: choices}
		}
		if len(matches) == 1 {
			a := matches[0]
			if a.review {
				return chargeInvalid("Resolve this load's trip selection and allocation totals in Gross Board before adding it")
			}
			driver, sourceDate, sourceSlot = a.driver, a.date, a.slot
			load.LoadTripID = a.trip
			load.DriverGross, load.OriginalRate, load.TotalMiles = a.gross, a.original, a.miles
		} else {
			if len(allocations) > 0 {
				return chargeInvalid("This load belongs to a different native driver or truck; review Gross Board assignments")
			}
			var split bool
			if err = tx.QueryRow(ctx, `SELECT split_expected OR jsonb_array_length(CASE WHEN jsonb_typeof(raw_payload->'trips')='array' THEN raw_payload->'trips' ELSE '[]'::jsonb END)>1 FROM loads WHERE id=$1`, in.LoadRecordID).Scan(&split); err != nil {
				return err
			}
			if split {
				return chargeInvalid("Select this load's trip in Gross Board before adding it to payroll")
			}
			if sourceDate == "" {
				return chargeInvalid("The load has no service date; review it in Gross Board before adding")
			}
			if kind == "investor" {
				var candidates []string
				if err = tx.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT driver_id::text),'{}') FROM truck_driver_assignments WHERE truck_id=$1 AND assigned_at<(($2::date+1)::timestamp AT TIME ZONE 'America/New_York') AND (unassigned_at IS NULL OR unassigned_at>=(($2::date+1)::timestamp AT TIME ZONE 'America/New_York'))`, in.DriverID, sourceDate).Scan(&candidates); err != nil {
					return err
				}
				if len(candidates) != 1 {
					return chargeInvalid("Review the load's dated driver assignment in Gross Board before adding it to Investor Pay")
				}
				driver = candidates[0]
			}
		}
		load.LoadRecordID = &in.LoadRecordID
		load.LoadNumber = in.LoadNumber
	}
	trip := 0
	if load.LoadTripID != nil {
		trip = *load.LoadTripID
	}
	if in.Action == "add" {
		var main string
		if err = tx.QueryRow(ctx, `SELECT payroll_driver_at($1::uuid,$2::date)::text`, driver, in.WeekStart).Scan(&main); err != nil {
			return err
		}
		if main != driver {
			return chargeInvalid("Review this driver's team change before adding historical loads to this week")
		}
	}
	// An unsplit legacy order may have competing native operators. A payroll
	// decision must not silently remove another person's allocation.
	var competing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+grossBoardEntriesSQL+` e WHERE NOT e.deleted AND e.day_status='' AND e.driver_id<>$3::uuid
 AND coalesce(e.load_trip_id,0)=$2 AND (e.load_record_id=$1 OR (e.load_record_id IS NULL AND lower(btrim(e.load_number))=lower(btrim($4)))))`, in.LoadRecordID, trip, driver, load.LoadNumber).Scan(&competing); err != nil {
		return err
	}
	if competing {
		return chargeInvalid("This load allocation has multiple native drivers; resolve its Gross Board allocations before changing payroll")
	}
	err = tx.QueryRow(ctx, `SELECT to_jsonb(p),week_start::text,driver_id::text,truck_id::text,version,included FROM payroll_load_placements p WHERE load_record_id=$1 AND load_trip_id=$2 FOR UPDATE`, in.LoadRecordID, trip).Scan(&before, &oldWeek, &oldDriver, &oldTruck, &oldVersion, &oldIncluded)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if in.Action != "add" && oldVersion != in.PlacementVersion {
		return ErrDriverPayConflict
	}
	// A previously recognized allocation may move only while BOTH destinations are open.
	if oldIncluded {
		if err = assertTransferSettlementsOpen(ctx, tx, oldDriver, oldTruck, oldWeek); err != nil {
			return err
		}
	}
	if truck == "" {
		var trucks []string
		if err = tx.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT truck_id::text),'{}') FROM truck_driver_assignments WHERE driver_id=$1 AND assigned_at<(($2::date+1)::timestamp AT TIME ZONE 'America/New_York') AND (unassigned_at IS NULL OR unassigned_at>=(($2::date+1)::timestamp AT TIME ZONE 'America/New_York'))`, driver, sourceDate).Scan(&trucks); err != nil {
			return err
		}
		if len(trucks) == 1 {
			truck = trucks[0]
		} else if len(trucks) > 1 {
			return chargeInvalid("Review conflicting dated truck assignments before changing this load")
		}
	}
	if kind == "investor" {
		truck = in.DriverID
	}
	var truckPtr *string
	if truck != "" {
		truckPtr = &truck
	}
	if err = assertTransferSettlementsOpen(ctx, tx, driver, truckPtr, in.WeekStart); err != nil {
		return err
	}
	var frozen bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (
 SELECT report FROM payroll_settlements WHERE finalized UNION ALL SELECT report FROM investor_pay_weeks WHERE finalized) s
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.report->'loads','[]')) l
 WHERE ((l->>'loadRecordId')::integer=$1 OR (l->>'loadRecordId' IS NULL AND lower(btrim(l->>'loadNumber'))=lower(btrim($3))))
 AND (coalesce((l->>'loadTripId')::integer,0)=$2 OR l->>'loadTripId' IS NULL OR $2=0))`, in.LoadRecordID, trip, load.LoadNumber).Scan(&frozen); err != nil {
		return err
	}
	if frozen {
		return chargeInvalid("This load is already included in a finalized driver or investor statement; reopen it first")
	}
	slot := load.Slot
	if in.Action == "add" {
		// Reject existing recognition, including another week; moving requires removal first.
		var included bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_load_placements WHERE load_record_id=$1 AND (load_trip_id=$2 OR load_trip_id=0 OR $2=0) AND included)`, in.LoadRecordID, trip).Scan(&included); err != nil {
			return err
		}
		if included {
			return chargeInvalid("This load is already manually included in a statement; remove that extra row before moving it")
		}
		for _, l := range card.Loads {
			if l.LoadRecordID != nil && *l.LoadRecordID == in.LoadRecordID && (l.LoadTripID == nil && trip == 0 || l.LoadTripID != nil && *l.LoadTripID == trip) {
				return chargeInvalid("This load is already included in this week's statement")
			}
		}
		if err = tx.QueryRow(ctx, `SELECT n FROM generate_series(0,99) n WHERE NOT EXISTS(SELECT 1 FROM payroll_load_placements WHERE driver_id=$1 AND week_start=$2::date AND slot=n AND included) ORDER BY n LIMIT 1`, driver, in.WeekStart).Scan(&slot); errors.Is(err, pgx.ErrNoRows) {
			return chargeInvalid("This statement already has 100 extra loads")
		} else if err != nil {
			return err
		}
	}
	if in.Action == "gross" {
		value, ok := newPayGross(in.DriverGross)
		if !ok {
			return chargeInvalid("Driver gross must be a nonnegative amount with up to two decimals")
		}
		load.DriverGross = value
	}
	included := in.Action != "remove"
	var after json.RawMessage
	err = tx.QueryRow(ctx, `INSERT INTO payroll_load_placements(load_record_id,load_trip_id,driver_id,truck_id,source_date,source_slot,week_start,slot,included,driver_gross,original_rate,miles)
 VALUES($1,$2,$3,$4,$5::date,$6,$7::date,$8,$9,nullif($10,'')::numeric,nullif($11,'')::numeric,nullif($12,'')::numeric)
 ON CONFLICT(load_record_id,load_trip_id) DO UPDATE SET driver_id=excluded.driver_id,truck_id=excluded.truck_id,source_date=excluded.source_date,source_slot=excluded.source_slot,week_start=excluded.week_start,slot=excluded.slot,included=excluded.included,driver_gross=excluded.driver_gross,
 original_rate=CASE WHEN $13='gross' THEN payroll_load_placements.original_rate ELSE excluded.original_rate END,
 miles=CASE WHEN $13='gross' THEN payroll_load_placements.miles ELSE excluded.miles END,version=payroll_load_placements.version+1
 RETURNING to_jsonb(payroll_load_placements)`, in.LoadRecordID, trip, driver, truckPtr, sourceDate, sourceSlot, in.WeekStart, slot, included, load.DriverGross, load.OriginalRate, load.TotalMiles, in.Action).Scan(&after)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO payroll_load_placement_history(actor_id,action,before_value,after_value) VALUES($1,$2,$3,$4)`, actor, in.Action, before, after); err != nil {
		return err
	}
	// Keep the draft visible when its final load is excluded, so the accountant
	// can still add the corrected load in the row below Sunday.
	if _, err = tx.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES(payroll_driver_at($1::uuid,$2::date),$2::date) ON CONFLICT DO NOTHING`, driver, in.WeekStart); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var payGrossPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,2})?$`)

func newPayGross(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !payGrossPattern.MatchString(value) {
		return "", false
	}
	return value, true
}
