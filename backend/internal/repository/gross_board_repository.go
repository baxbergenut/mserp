package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrGrossBoardConflict = errors.New("another dispatcher changed one of these days; reload the board before saving again")
var ErrGrossBoardTrip = errors.New("select a valid trip whose allocations reconcile with the system load; a trip cannot belong to two board drivers")

var ErrGrossBoardLoad = errors.New("the selected load no longer matches this load number; select it again")

type GrossBoardRepository struct{ pool *pgxpool.Pool }

func NewGrossBoardRepository(pool *pgxpool.Pool) *GrossBoardRepository {
	return &GrossBoardRepository{pool: pool}
}

type GrossBoardEntry struct {
	SplitReview            *LoadSplitReview `json:"splitReview,omitempty"`
	SourceDate             string           `json:"sourceDate,omitempty"`
	SourceSlot             int              `json:"sourceSlot,omitempty"`
	TransferVersion        int              `json:"transferVersion,omitempty"`
	TransferOut            bool             `json:"transferOut,omitempty"`
	CreditedDispatcherID   string           `json:"creditedDispatcherId,omitempty"`
	CreditedDispatcherName string           `json:"creditedDispatcherName,omitempty"`
	LoadTripID             *int             `json:"loadTripId"`
	Split                  bool             `json:"split"`
	TripReview             bool             `json:"tripReview"`
	TripReviewReason       string           `json:"tripReviewReason,omitempty"`
	Trips                  []GrossBoardTrip `json:"trips"`
	Slot                   int              `json:"slot"`
	Deleted                bool             `json:"deleted"`
	DriverID               string           `json:"driverId"`
	Date                   string           `json:"date"`
	LoadNumber             string           `json:"loadNumber"`
	Note                   *string          `json:"note,omitempty"` // nil on writes preserves the same load's note for older clients.
	DayStatus              string           `json:"dayStatus"`
	LoadRecordID           *int             `json:"loadRecordId"`
	OriginalRate           string           `json:"originalRate"`
	DriverRate             string           `json:"driverRate"`
	Miles                  string           `json:"miles"`
	Version                int              `json:"version"`
	EnteredOriginalRate    string           `json:"enteredOriginalRate"`
	EnteredMiles           string           `json:"enteredMiles"`
	SystemOriginalRate     string           `json:"systemOriginalRate"`
	SystemMiles            string           `json:"systemMiles"`
	Duplicate              bool             `json:"duplicate"`
	AcceptSystemValues     bool             `json:"acceptSystemValues,omitempty"`
}

type GrossBoardDriver struct {
	Assignments    []DailyAssignment `json:"assignments"`
	ID             string            `json:"id"`
	FullName       string            `json:"fullName"`
	TruckID        string            `json:"truckId"`
	TruckUnit      string            `json:"truckUnit"`
	DispatcherID   string            `json:"dispatcherId"`
	DispatcherName string            `json:"dispatcherName"`
	Active         bool              `json:"active"`
}

type GrossBoard struct {
	ManagedInSheets bool                `json:"managedInSheets,omitempty"`
	CutoverWeek     string              `json:"cutoverWeek,omitempty"`
	WeekStart       string              `json:"weekStart"`
	Drivers         []GrossBoardDriver  `json:"drivers"`
	Entries         []GrossBoardEntry   `json:"entries"`
	Balances        []GrossBoardBalance `json:"balances"`
}

type GrossBoardBalance struct {
	OpeningReference  string `json:"openingReference,omitempty"`
	DriverID          string `json:"driverId"`
	OpeningBalance    string `json:"openingBalance"`
	OpeningIncomplete int    `json:"openingIncomplete"`
}

type GrossBoardBalanceLine struct {
	Date         string `json:"date"`
	LoadNumber   string `json:"loadNumber"`
	OriginalRate string `json:"originalRate"`
	DriverRate   string `json:"driverRate"`
	Change       string `json:"change"`
	Balance      string `json:"balance"`
	Duplicate    bool   `json:"duplicate"`
}

type GrossBoardLoad struct {
	Trips            []GrossBoardTrip `json:"trips"`
	Split            bool             `json:"split"`
	TripReviewReason string           `json:"tripReviewReason,omitempty"`
	ID               int              `json:"id"`
	LoadNumber       string           `json:"loadNumber"`
	OriginalRate     string           `json:"originalRate"`
	Miles            string           `json:"miles"`
	DriverName       string           `json:"driverName"`
	PickupDate       string           `json:"pickupDate"`
}

// Slot zero preserves compatibility with the original daily entry table.
const grossBoardEntriesSQL = `(SELECT driver_id,service_date,0 AS slot,false AS deleted,
 load_number,load_record_id,original_rate,driver_rate,miles,version,entered_original_rate,entered_miles,day_status,note,load_trip_id FROM gross_board_entries g
 UNION ALL SELECT driver_id,service_date,slot,deleted,
 load_number,load_record_id,original_rate,driver_rate,miles,version,entered_original_rate,entered_miles,day_status,note,load_trip_id FROM gross_board_extra_entries g)`

// Resolve an explicit selection, or a unique exact number. Ambiguous numbers
// remain plans until a dispatcher chooses the intended upstream record.
const grossBoardResolvedLoad = `
 LEFT JOIN LATERAL (
   SELECT min(l.id) AS id FROM loads l
   WHERE NOT e.deleted AND e.day_status='' AND ((e.load_record_id IS NOT NULL AND l.id=e.load_record_id)
      OR (e.load_record_id IS NULL AND e.load_number<>'' AND lower(btrim(l.load_id))=lower(btrim(e.load_number))))
   HAVING count(*)=1
 ) matched ON true
 LEFT JOIN loads l ON l.id=matched.id `

// Entered values take precedence over current source values. Carry uses the same
// effective entries as the grid, with exact numeric arithmetic. A repeated
// system load for one driver contributes only on its earliest board date.
var grossBoardEffective = `WITH resolved AS (
 SELECT e.*, l.id AS matched_id,
 CASE WHEN l.id IS NOT NULL THEN l.load_id ELSE e.load_number END AS display_number,
 CASE WHEN trip_source.review THEN NULL ELSE CASE WHEN trip_source.reviewed THEN trip_source.original ELSE coalesce(e.entered_original_rate, CASE WHEN l.id IS NOT NULL THEN trip_source.original ELSE e.original_rate END) END END AS effective_original,
 CASE WHEN trip_source.review THEN NULL ELSE coalesce(e.entered_miles, CASE WHEN l.id IS NOT NULL THEN trip_source.miles ELSE e.miles END) END AS effective_miles,
 trip_source.original AS system_original, trip_source.miles AS system_miles,
 trip_source.split,trip_source.review AS trip_review,trip_source.review_reason AS trip_review_reason,trip_source.options AS trips,
 l.id IS NOT NULL AND row_number() OVER (
   PARTITION BY e.driver_id, l.id,e.load_trip_id ORDER BY e.source_date,e.source_slot) > 1 AS duplicate
 FROM ` + dispatchBoardEntriesSQL + ` e ` + grossBoardResolvedLoad + loadTripSourceSQL + `
 WHERE e.source_date < $1::date+7
), effective AS (
 SELECT *, CASE WHEN NOT deleted AND day_status='' AND NOT duplicate AND btrim(load_number)<>'' AND
 effective_original IS NOT NULL AND driver_rate IS NOT NULL
 THEN effective_original-driver_rate END AS balance_change
 FROM resolved WHERE service_date < $1::date+7
) `

const grossBoardEntryColumns = `driver_id, service_date::text, display_number,
 matched_id, coalesce(effective_original::text,''), coalesce(driver_rate::text,''),
 coalesce(effective_miles::text,''), version, coalesce(entered_original_rate::text,''),
 coalesce(entered_miles::text,''), duplicate, day_status, slot, deleted,
 coalesce(system_original::text,''), coalesce(system_miles::text,''), note,load_trip_id,split,trip_review,trip_review_reason,trips,source_date::text,source_slot,transfer_version,credited_dispatcher_id,credited_dispatcher_name`

func scanGrossBoardEntry(row pgx.Row, e *GrossBoardEntry) error {
	var trips []byte
	err := row.Scan(&e.DriverID, &e.Date, &e.LoadNumber, &e.LoadRecordID,
		&e.OriginalRate, &e.DriverRate, &e.Miles, &e.Version,
		&e.EnteredOriginalRate, &e.EnteredMiles, &e.Duplicate, &e.DayStatus, &e.Slot, &e.Deleted,
		&e.SystemOriginalRate, &e.SystemMiles, &e.Note, &e.LoadTripID, &e.Split, &e.TripReview, &e.TripReviewReason, &trips, &e.SourceDate, &e.SourceSlot, &e.TransferVersion, &e.CreditedDispatcherID, &e.CreditedDispatcherName)
	if err != nil {
		return err
	}
	e.DayStatus = LegacyGrossBoardDayStatus(e.DayStatus)
	return json.Unmarshal(trips, &e.Trips)
}

func (r *GrossBoardRepository) Get(ctx context.Context, week time.Time) (GrossBoard, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return GrossBoard{}, err
	}
	defer tx.Rollback(ctx)
	board, err := readGrossBoard(ctx, tx, week)
	if err != nil {
		return board, err
	}
	return board, tx.Commit(ctx)
}

// Payroll projections share the exact board resolution and transaction snapshot.
func readGrossBoard(ctx context.Context, tx pgx.Tx, week time.Time) (GrossBoard, error) {
	board := GrossBoard{WeekStart: week.Format("2006-01-02"), Drivers: []GrossBoardDriver{}, Entries: []GrossBoardEntry{}, Balances: []GrossBoardBalance{}}
	rows, err := tx.Query(ctx, `SELECT d.id, d.full_name, coalesce(t.id::text,''), coalesce(t.unit_number,''),
 coalesce(dp.id::text,''), coalesce(dp.full_name,historical_dispatcher.dispatcher_name,'Unassigned'), d.active
 FROM drivers d `+weeklyAssignmentJoins+`
 WHERE payroll_driver_at(d.id,$1::date)=d.id AND ((d.active AND `+weeklyDriverStartedSQL+`) OR EXISTS (SELECT 1 FROM `+dispatchBoardEntriesSQL+` e WHERE NOT e.deleted AND e.driver_id=d.id
 AND e.service_date >= $1::date AND e.service_date < $1::date+7) OR EXISTS(SELECT 1 FROM gross_board_transfers moved WHERE moved.driver_id=d.id AND moved.active AND moved.source_date >= $1::date AND moved.source_date < $1::date+7))
 ORDER BY dp.full_name NULLS LAST, dp.id, d.full_name, d.id`, week)
	if err != nil {
		return board, err
	}
	for rows.Next() {
		var d GrossBoardDriver
		if err = rows.Scan(&d.ID, &d.FullName, &d.TruckID, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName, &d.Active); err != nil {
			rows.Close()
			return board, err
		}
		board.Drivers = append(board.Drivers, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return board, err
	}

	rows, err = tx.Query(ctx, grossBoardEffective+"SELECT "+grossBoardEntryColumns+`
 FROM effective WHERE service_date >= $1::date ORDER BY driver_id,service_date,slot`, week)
	if err != nil {
		return board, err
	}
	for rows.Next() {
		var e GrossBoardEntry
		if err = scanGrossBoardEntry(rows, &e); err != nil {
			rows.Close()
			return board, err
		}
		board.Entries = append(board.Entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return board, err
	}
	rows, err = tx.Query(ctx, grossBoardEffective+`SELECT driver_id,
 coalesce(sum(balance_change),0)::text,
 count(*) FILTER (WHERE NOT deleted AND day_status='' AND balance_change IS NULL AND NOT duplicate
 AND (load_number<>'' OR effective_original IS NOT NULL OR driver_rate IS NOT NULL))::int
 FROM effective WHERE service_date < $1::date GROUP BY driver_id`, week)
	if err != nil {
		return board, err
	}
	defer rows.Close()
	for rows.Next() {
		var balance GrossBoardBalance
		if err = rows.Scan(&balance.DriverID, &balance.OpeningBalance, &balance.OpeningIncomplete); err != nil {
			return board, err
		}
		board.Balances = append(board.Balances, balance)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return board, err
	}
	markers, err := tx.Query(ctx, `SELECT t.driver_id::text,t.source_date::text,t.source_slot,t.version,t.dispatcher_id,t.dispatcher_name,e.load_number,e.version FROM gross_board_transfers t JOIN `+grossBoardEntriesSQL+` e ON e.driver_id=t.driver_id AND e.service_date=t.source_date AND e.slot=t.source_slot WHERE t.active AND t.source_date >= $1::date AND t.source_date < $1::date+7`, week)
	if err != nil {
		return board, err
	}
	for markers.Next() {
		var e GrossBoardEntry
		e.TransferOut = true
		e.Duplicate = true
		if err = markers.Scan(&e.DriverID, &e.Date, &e.Slot, &e.TransferVersion, &e.CreditedDispatcherID, &e.CreditedDispatcherName, &e.LoadNumber, &e.Version); err != nil {
			markers.Close()
			return board, err
		}
		e.SourceDate = e.Date
		e.SourceSlot = e.Slot
		e.Trips = []GrossBoardTrip{}
		board.Entries = append(board.Entries, e)
	}
	err = markers.Err()
	markers.Close()
	if err != nil {
		return board, err
	}
	vacation, err := readVacationState(ctx, tx, board.WeekStart, false)
	if err != nil {
		return board, err
	}
	working := map[string]bool{}
	for _, e := range board.Entries {
		if !e.Deleted && (e.TransferOut || e.DayStatus == "" && strings.TrimSpace(e.LoadNumber) != "" || e.DayStatus == "SHOP") {
			working[e.DriverID] = true
		}
	}
	visible := []GrossBoardDriver{}
	for _, d := range board.Drivers {
		if !vacation.drivers[d.ID] || working[d.ID] {
			visible = append(visible, d)
		}
	}
	board.Drivers = visible
	ids := make([]string, 0, len(board.Drivers))
	for _, d := range board.Drivers {
		ids = append(ids, d.ID)
	}
	assignments, err := readDailyAssignments(ctx, tx, board.WeekStart, ids)
	if err != nil {
		return board, err
	}
	for i := range board.Drivers {
		board.Drivers[i].Assignments = assignments[board.Drivers[i].ID]
	}
	return board, nil
}

func (r *GrossBoardRepository) BalanceHistory(ctx context.Context, driverID string, week time.Time) ([]GrossBoardBalanceLine, error) {
	rows, err := r.pool.Query(ctx, grossBoardEffective+`SELECT service_date::text, display_number,
 coalesce(effective_original::text,''), coalesce(driver_rate::text,''),
 coalesce(balance_change::text,''),
 (coalesce(sum(balance_change) OVER (ORDER BY service_date,slot ROWS UNBOUNDED PRECEDING),0))::text, duplicate
 FROM effective WHERE driver_id=$2 AND NOT deleted AND day_status='' AND
 (load_number<>'' OR effective_original IS NOT NULL OR driver_rate IS NOT NULL)
 ORDER BY service_date,slot`, week, driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GrossBoardBalanceLine{}
	for rows.Next() {
		var line GrossBoardBalanceLine
		if err = rows.Scan(&line.Date, &line.LoadNumber, &line.OriginalRate, &line.DriverRate, &line.Change, &line.Balance, &line.Duplicate); err != nil {
			return nil, err
		}
		result = append(result, line)
	}
	return result, rows.Err()
}

func (r *GrossBoardRepository) SearchLoads(ctx context.Context, search string) ([]GrossBoardLoad, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,load_id,total_pay::text,coalesce(total_miles::text,''),
 coalesce(driver_name,''),coalesce((coalesce(pickup_time,pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,''),trip_source.options,trip_source.split,trip_source.review_reason
 FROM (SELECT c.*,to_jsonb(source)->'split_allocation' split_allocation FROM clustie_loads c JOIN loads source ON source.id=c.id) l CROSS JOIN (SELECT NULL::integer load_trip_id) e `+loadTripSourceSQL+` WHERE load_id<>'' AND strpos(lower(load_id),lower($1))>0
 ORDER BY (lower(btrim(load_id))=lower(btrim($1))) DESC,id DESC LIMIT 20`, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GrossBoardLoad{}
	for rows.Next() {
		var l GrossBoardLoad
		var trips []byte
		if err = rows.Scan(&l.ID, &l.LoadNumber, &l.OriginalRate, &l.Miles, &l.DriverName, &l.PickupDate, &trips, &l.Split, &l.TripReviewReason); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(trips, &l.Trips); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

// Save only edited days, atomically, with optimistic concurrency. Empty days
// retain a version so clearing a day cannot allow stale editors to overwrite it.
func (r *GrossBoardRepository) Save(ctx context.Context, entries []GrossBoardEntry) error {
	_, err := r.SaveEntries(ctx, entries)
	return err
}

func (r *GrossBoardRepository) SaveEntries(ctx context.Context, entries []GrossBoardEntry, actors ...string) ([]GrossBoardEntry, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	actor := ""
	if len(actors) > 0 {
		actor = actors[0]
	}
	if err = setBoardActor(ctx, tx, actor, "gross_board"); err != nil {
		return nil, err
	}
	// Preserve the existing fleet-status then financial lock order.
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return nil, err
	}
	entries, err = prepareSplitReviews(ctx, tx, entries)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.DriverID+a.Date == b.DriverID+b.Date {
			return a.Slot < b.Slot
		}
		return a.DriverID+a.Date < b.DriverID+b.Date
	})
	for index, e := range entries {
		var transferred bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gross_board_transfers WHERE driver_id=$1 AND active AND ((source_date=$2::date AND source_slot=$3) OR (dispatch_date=$2::date AND dispatch_slot=$3)))`, e.DriverID, e.Date, e.Slot).Scan(&transferred); err != nil {
			return nil, err
		}
		if transferred {
			return nil, chargeInvalid("Manage or restore this transferred load in Driver Pay or Investor Pay before editing it")
		}

		if e.Deleted || e.DayStatus != "" || e.LoadNumber == "" {
			empty := ""
			e.Note = &empty
			e.LoadTripID = nil
		}
		if !e.Deleted && e.LoadNumber != "" && e.DayStatus == "" {
			var previous string
			if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT load_number FROM gross_board_entries WHERE driver_id=$1 AND service_date=$2::date AND $3=0 UNION ALL SELECT load_number FROM gross_board_extra_entries WHERE driver_id=$1 AND service_date=$2::date AND slot=$3 AND NOT deleted LIMIT 1),'')`, e.DriverID, e.Date, e.Slot).Scan(&previous); err != nil {
				return nil, err
			}
			if !strings.EqualFold(strings.TrimSpace(previous), strings.TrimSpace(e.LoadNumber)) && e.Date >= boardLoadToday(time.Now()) {
				if err := requireDispatchableTruck(ctx, tx, e.DriverID); err != nil {
					return nil, err
				}
			}
		}
		var loadID *int
		if e.LoadRecordID != nil {
			var id int
			err = tx.QueryRow(ctx, `SELECT id FROM loads WHERE id=$1 AND lower(btrim(load_id))=lower(btrim($2))`, e.LoadRecordID, e.LoadNumber).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrGrossBoardLoad
			}
			if err != nil {
				return nil, err
			}
			loadID = &id
		} else if e.LoadNumber != "" {
			err = tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(id) END FROM loads WHERE lower(btrim(load_id))=lower(btrim($1))`, e.LoadNumber).Scan(&loadID)
			if err != nil {
				return nil, err
			}
		}

		enteredOriginal, enteredMiles := e.EnteredOriginalRate, e.EnteredMiles
		if enteredOriginal == "" {
			enteredOriginal = e.OriginalRate
		}
		if enteredMiles == "" {
			enteredMiles = e.Miles
		}
		// Preserve an existing selection for older clients that omit the field.
		var previousTrip *int
		var oldTrip *int
		if err = tx.QueryRow(ctx, `SELECT (SELECT load_trip_id FROM `+grossBoardEntriesSQL+` p WHERE p.driver_id=$1 AND p.service_date=$2::date AND p.slot=$3 AND NOT p.deleted)`, e.DriverID, e.Date, e.Slot).Scan(&oldTrip); err != nil {
			return nil, err
		}
		if oldTrip != nil {
			if err = assertPaySourceOpen(ctx, tx, PaySourceAcceptance{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot}); err != nil {
				return nil, err
			}
		}
		if err = tx.QueryRow(ctx, `SELECT (SELECT load_trip_id FROM `+grossBoardEntriesSQL+` p WHERE p.driver_id=$1 AND p.service_date=$2::date AND p.slot=$3 AND NOT p.deleted AND p.load_record_id=$4)`, e.DriverID, e.Date, e.Slot, loadID).Scan(&previousTrip); err != nil {
			return nil, err
		}
		if e.LoadTripID == nil && !e.Deleted && e.DayStatus == "" {
			e.LoadTripID = previousTrip
		}
		original, miles := e.OriginalRate, e.Miles
		split, review, reviewed := false, false, false
		reviewReason := ""
		if loadID != nil {
			// Lock the source before comparing the displayed allocation.
			if _, err = tx.Exec(ctx, `SELECT id FROM loads WHERE id=$1 FOR SHARE`, *loadID); err != nil {
				return nil, err
			}
			err = tx.QueryRow(ctx, `SELECT coalesce(trip_source.original::text,''),coalesce(trip_source.miles::text,''),trip_source.split,trip_source.review,trip_source.review_reason,trip_source.reviewed FROM loads l CROSS JOIN (SELECT $2::integer load_trip_id) e `+loadTripSourceSQL+` WHERE l.id=$1`, *loadID, e.LoadTripID).Scan(&original, &miles, &split, &review, &reviewReason, &reviewed)
			if err != nil {
				return nil, err
			}
		}
		if e.LoadTripID != nil {
			if loadID == nil || review {
				if reviewReason != "" {
					return nil, chargeInvalid("%s", reviewReason)
				}
				return nil, ErrGrossBoardTrip
			}
			if err = assertPaySourceOpen(ctx, tx, PaySourceAcceptance{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot}); err != nil {
				return nil, err
			}
			var occupied bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+grossBoardEntriesSQL+` b WHERE NOT b.deleted AND b.day_status='' AND b.load_record_id=$1 AND b.load_trip_id=$2 AND b.driver_id<>$3)`, *loadID, *e.LoadTripID, e.DriverID).Scan(&occupied); err != nil {
				return nil, err
			}
			if occupied {
				return nil, chargeInvalid("This trip is already selected for another driver. Choose the trip performed by this driver.")
			}
			if (previousTrip == nil || *previousTrip != *e.LoadTripID) && !e.AcceptSystemValues {
				return nil, ErrGrossBoardTrip
			}
		}
		if loadID == nil {
			enteredOriginal, enteredMiles = original, miles
		} else if e.AcceptSystemValues {
			if review {
				if reviewReason != "" {
					return nil, chargeInvalid("%s", reviewReason)
				}
				return nil, ErrGrossBoardTrip
			}
			var unchanged bool
			if err = tx.QueryRow(ctx, `SELECT nullif($1,'')::numeric IS NOT DISTINCT FROM nullif($2,'')::numeric AND nullif($3,'')::numeric IS NOT DISTINCT FROM nullif($4,'')::numeric`, original, e.OriginalRate, miles, e.Miles).Scan(&unchanged); err != nil {
				return nil, err
			}
			if !unchanged {
				return nil, ErrGrossBoardConflict
			}
			enteredOriginal, enteredMiles = original, miles
		}
		if reviewed && e.LoadTripID != nil {
			if enteredOriginal != "" {
				a, err := chargeCents(enteredOriginal)
				b, _ := chargeCents(original)
				if err != nil || a != b {
					return nil, chargeInvalid("Edit original gross in the split editor so the whole load stays balanced")
				}
			}
			enteredOriginal = original
		}
		// Exact-number resolution can beat the browser's load lookup. Apply the
		// same initial driver-gross default here, preserving explicit amounts
		// (including zero) and later edits to an already linked entry.
		if !split && loadID != nil && e.DriverRate == "" && (e.LoadRecordID == nil || e.Version == 0) {
			e.DriverRate = enteredOriginal
			if e.DriverRate == "" {
				e.DriverRate = original
			}
		}
		var version int
		if e.Slot > 0 {
			if e.Version == 0 {
				err = tx.QueryRow(ctx, `INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,original_rate,driver_rate,miles,deleted,entered_original_rate,entered_miles,day_status,note,load_trip_id)
 VALUES($1,$2::date,$3,$4,$5,NULLIF($6,'')::numeric,NULLIF($7,'')::numeric,NULLIF($8,'')::numeric,$9,NULLIF($10,'')::numeric,NULLIF($11,'')::numeric,$12,coalesce($13,''),$14)
 ON CONFLICT DO NOTHING RETURNING version`, e.DriverID, e.Date, e.Slot, e.LoadNumber, loadID, original, e.DriverRate, miles, e.Deleted, enteredOriginal, enteredMiles, e.DayStatus, e.Note, e.LoadTripID).Scan(&version)
			} else {
				err = tx.QueryRow(ctx, `UPDATE gross_board_extra_entries SET load_number=$4,load_record_id=$5,original_rate=NULLIF($6,'')::numeric,
 driver_rate=NULLIF($7,'')::numeric,miles=NULLIF($8,'')::numeric,deleted=$9,version=version+1,updated_at=now(),
 entered_original_rate=NULLIF($11,'')::numeric,entered_miles=NULLIF($12,'')::numeric,day_status=$13, note=CASE WHEN $9 OR $4='' OR $13<>'' THEN ''
 WHEN lower(btrim(load_number))=lower(btrim($4)) AND (load_record_id IS NULL OR $5::integer IS NULL OR load_record_id=$5) THEN coalesce($14,note) ELSE coalesce($14,'') END,load_trip_id=$15
 WHERE driver_id=$1 AND service_date=$2::date AND slot=$3 AND version=$10 RETURNING version`,
					e.DriverID, e.Date, e.Slot, e.LoadNumber, loadID, original, e.DriverRate, miles, e.Deleted, e.Version, enteredOriginal, enteredMiles, e.DayStatus, e.Note, e.LoadTripID).Scan(&version)
			}
		} else if e.Version == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,driver_rate,miles,entered_original_rate,entered_miles,day_status,note,load_trip_id)
     VALUES($1,$2::date,$3,$4,NULLIF($5,'')::numeric,NULLIF($6,'')::numeric,NULLIF($7,'')::numeric,NULLIF($8,'')::numeric,NULLIF($9,'')::numeric,$10,coalesce($11,''),$12)
     ON CONFLICT DO NOTHING RETURNING version`, e.DriverID, e.Date, e.LoadNumber, loadID, original, e.DriverRate, miles, enteredOriginal, enteredMiles, e.DayStatus, e.Note, e.LoadTripID).Scan(&version)
		} else {
			err = tx.QueryRow(ctx, `UPDATE gross_board_entries SET load_number=$3,load_record_id=$4,
     original_rate=NULLIF($5,'')::numeric,driver_rate=NULLIF($6,'')::numeric,miles=NULLIF($7,'')::numeric,
     entered_original_rate=NULLIF($9,'')::numeric,entered_miles=NULLIF($10,'')::numeric,day_status=$11,
     note=CASE WHEN $3='' OR $11<>'' THEN ''
       WHEN lower(btrim(load_number))=lower(btrim($3)) AND (load_record_id IS NULL OR $4::integer IS NULL OR load_record_id=$4) THEN coalesce($12,note) ELSE coalesce($12,'') END,load_trip_id=$13,
     version=version+1,updated_at=now() WHERE driver_id=$1 AND service_date=$2::date AND version=$8 RETURNING version`,
				e.DriverID, e.Date, e.LoadNumber, loadID, original, e.DriverRate, miles, e.Version, enteredOriginal, enteredMiles, e.DayStatus, e.Note, e.LoadTripID).Scan(&version)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGrossBoardConflict
		}
		if err != nil {
			return nil, err
		}
		e.LoadRecordID = loadID
		e.OriginalRate = original
		e.Miles = miles
		e.Version = version
		e.EnteredOriginalRate, e.EnteredMiles = enteredOriginal, enteredMiles
		e.AcceptSystemValues = false
		entries[index] = e
	}

	progressIDs := []string{}
	for _, entry := range entries {
		progressIDs = append(progressIDs, entry.DriverID)
	}
	if err = syncFleetStatusProgress(ctx, tx, progressIDs); err != nil {
		return nil, err
	}
	for i := range entries {
		date, err := time.Parse(time.DateOnly, entries[i].Date)
		if err != nil {
			return nil, err
		}
		err = scanGrossBoardEntry(tx.QueryRow(ctx, grossBoardEffective+"SELECT "+grossBoardEntryColumns+
			" FROM effective WHERE driver_id=$2 AND service_date=$3::date AND slot=$4", date, entries[i].DriverID, entries[i].Date, entries[i].Slot), &entries[i])
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return entries, nil
}
