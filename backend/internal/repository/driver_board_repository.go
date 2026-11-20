package repository

import (
	"context"
	"errors"
	"mserp/internal/boardroutes"
	"mserp/internal/fiveeld"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

var ErrDriverBoardConflict = errors.New("status board changed; reload to review the latest values before editing again")

type DriverBoardEntry struct {
	loadActionVersion int
	FleetStatus       *BoardFleetStatus `json:"fleetStatus,omitempty"`
	StatusReason      string            `json:"statusReason,omitempty"`
	LeavePlan         *DriverLeavePlan  `json:"leavePlan,omitempty"`
	EffectiveAt       *time.Time        `json:"effectiveAt,omitempty"`
	UndoID            int64             `json:"undoId,omitempty"`
	StatusEdited      bool              `json:"statusEdited,omitempty"`
	EditedFields      []string          `json:"editedFields,omitempty"` // Write-only field mask for inline edits.
	// DispatchOnly leaves the committed Notes and trailer cells untouched.
	DispatchOnly bool `json:"dispatchOnly,omitempty"`
	// Selecting the same number should still resolve its stops.
	ResolveCurrentLoad bool   `json:"resolveCurrentLoad,omitempty"`
	DriverID           string `json:"driverId"`
	CurrentLoad        string `json:"currentLoad"`
	TrailerNumber      string `json:"trailerNumber"`
	TrailerNote        string `json:"trailerNote"`
	Status             string `json:"status"`
	Destination        string `json:"destination"`
	ETA                string `json:"eta"`
	Notes              string `json:"notes"`
	HomeTime           string `json:"homeTime"`
	Version            int    `json:"version"`
}

type DriverBoardDriver struct {
	Departed                   bool                 `json:"departed,omitempty"`
	AvailabilityStatus         string               `json:"availabilityStatus"`
	StatusVersion              int                  `json:"statusVersion"`
	TruckOperatingStatus       string               `json:"truckOperatingStatus"`
	DispatcherExtension        *int                 `json:"dispatcherExtension"`
	MainUpdaterName            string               `json:"mainUpdaterName"`
	MainUpdaterExtension       *int                 `json:"mainUpdaterExtension"`
	AfterHoursUpdaterName      string               `json:"afterHoursUpdaterName"`
	AfterHoursUpdaterExtension *int                 `json:"afterHoursUpdaterExtension"`
	ID                         string               `json:"id"`
	FullName                   string               `json:"fullName"`
	DriverType                 string               `json:"driverType"`
	TruckUnit                  string               `json:"truckUnit"`
	TruckID                    string               `json:"truckId"`
	Phone                      string               `json:"phone"`
	City                       string               `json:"city"`
	State                      string               `json:"state"`
	DispatcherID               string               `json:"dispatcherId"`
	DispatcherName             string               `json:"dispatcherName"`
	Location                   *DriverBoardLocation `json:"location"`
}

type DriverBoardLocation struct {
	Source              string    `json:"source"`
	Heading             *float64  `json:"heading"`
	Latitude            float64   `json:"latitude"`
	Longitude           float64   `json:"longitude"`
	ReportedAt          time.Time `json:"reportedAt"`
	ProviderTruckNumber string    `json:"providerTruckNumber"`
}

type FiveELDBoardSummary struct {
	Configured    bool       `json:"configured"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	LastError     string     `json:"lastError"`
	Unmatched     int        `json:"unmatched"`
	Ambiguous     int        `json:"ambiguous"`
	Invalid       int        `json:"invalid"`
}

type DriverBoard struct {
	PendingDrivers          []BoardPendingDriver  `json:"pendingDrivers"`
	StatusSyncEnabled       bool                  `json:"statusSyncEnabled"`
	AutoOrderDispatchers    map[string]bool       `json:"autoOrderDispatchers"`
	AutoOrderGroupNames     map[string]string     `json:"autoOrderGroupNames,omitempty"`
	DriverOrderRevisions    map[string]string     `json:"driverOrderRevisions"`
	DispatcherOrderRevision string                `json:"dispatcherOrderRevision"`
	WeekStart               string                `json:"weekStart"`
	Drivers                 []DriverBoardDriver   `json:"drivers"`
	Entries                 []DriverBoardEntry    `json:"entries"`
	GrossEntries            []GrossBoardEntry     `json:"grossEntries"`
	GrossTierMinimumCents   []int64               `json:"grossTierMinimumCents,omitempty"`
	Loads                   map[string]BoardLoads `json:"loads"`
	ELD                     FiveELDBoardSummary   `json:"eld"`
}

type DriverBoardRepository struct {
	loadSources       *boardLoadSourceCaches
	weekEntries       *boardWeekCache[[]GrossBoardEntry]
	workPriority      *PayrollWorkPriority
	retryRoutes       *boardroutes.Client
	retryELD          *fiveeld.Client
	retryUSDOT        string
	pool              *pgxpool.Pool
	now               func() time.Time
	fiveELDConfigured bool
}

func NewDriverBoardRepository(pool *pgxpool.Pool, fiveELDConfigured ...bool) *DriverBoardRepository {
	configured := len(fiveELDConfigured) > 0 && fiveELDConfigured[0]
	return &DriverBoardRepository{pool: pool, loadSources: new(boardLoadSourceCaches), weekEntries: new(boardWeekCache[[]GrossBoardEntry]), now: time.Now, fiveELDConfigured: configured}
}

// Ownership and pay method are independent. An owner driving their own truck
// is not a hired driver on an investor truck, regardless of their profile flag.
func driverBoardType(payType string, ownerOperator, investorTruck, ownTruck bool) string {
	if investorTruck && !ownTruck {
		if payType == "cpm" {
			return "M-O"
		}
		return "%-O"
	}
	if payType == "cpm" {
		return "M"
	}
	if ownerOperator || ownTruck {
		return "O"
	}
	return "%"
}

// Match fleet_truck_operating_status. Callers supply the final in_use/ready
// decision when they already know which active drivers operate the truck.
const boardTruckConditionSQL = `CASE WHEN t.id IS NULL THEN '' WHEN t.status='terminated' THEN 'terminated'
 WHEN t.status='out_of_service' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='out_of_service') THEN 'out_of_service'
 WHEN t.status='maintenance' OR EXISTS(SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status='shop') THEN 'shop'`

func (r *DriverBoardRepository) Get(ctx context.Context, week time.Time) (DriverBoard, error) {
	return r.get(ctx, week, true, nil)
}

// SharedDriverBoard contains a fleet snapshot and its onboarding audience from
// the same repeatable-read transaction. Only the viewer projection is serializable.
type SharedDriverBoard struct {
	board          DriverBoard
	pendingViewers []string
}

func (s SharedDriverBoard) ForViewer(id string, administrator bool) DriverBoard {
	board := s.board
	if !administrator && !slices.Contains(s.pendingViewers, id) {
		board.PendingDrivers = []BoardPendingDriver{}
	}
	return board
}

func (r *DriverBoardRepository) GetShared(ctx context.Context, week time.Time) (SharedDriverBoard, error) {
	var snapshot SharedDriverBoard
	board, err := r.get(WithTaskViewer(ctx, "", true), week, true, &snapshot.pendingViewers)
	snapshot.board = board
	return snapshot, err
}

func (r *DriverBoardRepository) get(ctx context.Context, week time.Time, displayBoard bool, pendingViewers *[]string) (DriverBoard, error) {
	result := DriverBoard{WeekStart: week.Format("2006-01-02"), Drivers: []DriverBoardDriver{}, Entries: []DriverBoardEntry{}, GrossEntries: []GrossBoardEntry{}}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	key, err := boardWeekRevision(ctx, tx, week, false)
	if err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&result.StatusSyncEnabled); err != nil {
		return result, err
	}
	if displayBoard {
		rule, err := readEffectiveCommissionRule(ctx, tx, result.WeekStart)
		if err != nil {
			return result, err
		}
		if rule != nil {
			for _, tier := range rule.Rules.Tiers {
				result.GrossTierMinimumCents = append(result.GrossTierMinimumCents, tier.minGrossCents())
			}
		}
	}
	// Retained departure rows are history, not live assignments.
	location, _ := time.LoadLocation("America/New_York")
	today := r.now().In(location).Format(time.DateOnly)
	endRoster := db.Phase(ctx, "status_roster")
	rows, err := tx.Query(ctx, `SELECT d.id,d.full_name,d.pay_type,coalesce(i.driver_id=d.id,false),
 coalesce(NOT i.is_company,false),coalesce(i.driver_id=d.id,false),coalesce(t.unit_number,''),coalesce(t.id::text,''),coalesce(d.phone,''),
 coalesce(dp.id::text,''),coalesce(dp.full_name,'Unassigned'),
 dp.extension,coalesce(mu.full_name,''),mu.extension,coalesce(au.full_name,''),au.extension,
 coalesce(b.current_load,''),coalesce(b.trailer_number,''),coalesce(b.trailer_note,''),coalesce(b.status,''),coalesce(b.destination,''),
	 coalesce(b.eta,''),coalesce(b.notes,''),coalesce(b.home_time,''),coalesce(b.version,0),coalesce(b.leave_plan,'{}'::jsonb),
	 el.latitude,el.longitude,el.reported_at,el.provider_truck_number,el.heading,el.source,
 `+boardTruckConditionSQL+`
 ELSE 'in_use' END,d.availability_status,d.status_version,coalesce(d.city,''),coalesce(d.state,''),NOT d.active
 FROM drivers d CROSS JOIN (SELECT $1::date AS today) board_day
 `+driverBoardDispatcherJoinSQL+`
 LEFT JOIN dispatchers dp ON dp.id=`+driverBoardDispatcherSQL+`
 LEFT JOIN dispatcher_updaters ma ON ma.dispatcher_id=dp.id AND ma.shift='main'
 LEFT JOIN updaters mu ON mu.id=ma.updater_id
 LEFT JOIN dispatcher_updaters aa ON aa.dispatcher_id=dp.id AND aa.shift='after_hours'
 LEFT JOIN updaters au ON au.id=aa.updater_id
 `+driverBoardTruckJoinSQL+`
 LEFT JOIN trucks t ON t.id=a.truck_id LEFT JOIN investors i ON i.id=t.owner_id
 LEFT JOIN driver_board b ON b.driver_id=d.id
 `+statusBoardELDLocationJoinSQL+`
 WHERE `+driverBoardRosterPredicateSQL+`
 ORDER BY dp.full_name NULLS LAST,dp.id,d.full_name,d.id`, today)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var d DriverBoardDriver
		var e DriverBoardEntry
		var pay string
		var owner, investor, own bool
		var providerTruckNumber, locationSource *string
		var latitude, longitude, heading *float64
		var reportedAt *time.Time
		err = rows.Scan(&d.ID, &d.FullName, &pay, &owner, &investor, &own, &d.TruckUnit, &d.TruckID, &d.Phone, &d.DispatcherID, &d.DispatcherName,
			&d.DispatcherExtension, &d.MainUpdaterName, &d.MainUpdaterExtension, &d.AfterHoursUpdaterName, &d.AfterHoursUpdaterExtension,
			&e.CurrentLoad, &e.TrailerNumber, &e.TrailerNote, &e.Status, &e.Destination, &e.ETA, &e.Notes, &e.HomeTime, &e.Version, &e.LeavePlan,
			&latitude, &longitude, &reportedAt, &providerTruckNumber, &heading, &locationSource, &d.TruckOperatingStatus, &d.AvailabilityStatus, &d.StatusVersion, &d.City, &d.State, &d.Departed)
		if err != nil {
			rows.Close()
			return result, err
		}
		d.DriverType = driverBoardType(pay, owner, investor, own)
		if latitude != nil && longitude != nil && reportedAt != nil && providerTruckNumber != nil {
			d.Location = &DriverBoardLocation{Latitude: *latitude, Longitude: *longitude, Heading: heading,
				ReportedAt: *reportedAt, ProviderTruckNumber: *providerTruckNumber, Source: pmRecordID(locationSource)}
		}
		if d.Departed {
			e.Status = "Left"
			d.Location = nil
		}
		e.DriverID = d.ID
		result.Drivers = append(result.Drivers, d)
		result.Entries = append(result.Entries, e)
	}
	err = rows.Err()
	rows.Close()
	endRoster()
	if err != nil {
		return result, err
	}
	ids := []string{}
	for _, d := range result.Drivers {
		ids = append(ids, d.ID)
	}
	if displayBoard {
		endEntries := db.Phase(ctx, "board_week_entries")
		entries, err := r.weekEntries.get(ctx, key, cloneWeekEntries, func() ([]GrossBoardEntry, error) { return readBoardWeekEntries(ctx, tx, week) })
		endEntries()
		if err != nil {
			return result, err
		}
		visible := make(map[string]bool, len(ids))
		for _, id := range ids {
			visible[id] = true
		}
		for _, entry := range entries {
			if visible[entry.DriverID] {
				result.GrossEntries = append(result.GrossEntries, entry)
			}
		}
		result.ELD, err = fiveELDSyncSummary(ctx, tx)
		if err != nil {
			return result, err
		}
		result.ELD.Configured = r.fiveELDConfigured
	}
	endLoads := db.Phase(ctx, "status_load_views")
	result.Loads, err = readBoardLoadViewsCached(ctx, tx, ids, r.loadStartDate(""), r.now(), boardLoadViewOptions{jitDisabled: true}, r.loadSources, key)
	endLoads()
	if err != nil {
		return result, err
	}
	if displayBoard {
		endCities := db.Phase(ctx, "status_stop_cities")
		err = applyBoardStopCities(ctx, tx, result.Loads)
		endCities()
		if err != nil {
			return result, err
		}
	}
	for i := range result.Entries {
		e := &result.Entries[i]
		if !result.StatusSyncEnabled {
			e.Status = boardProgressStatus(e.Status, result.Loads[e.DriverID].Next)
		}
		if a := result.Loads[e.DriverID].Automation; a != nil && !a.ManualETAValid {
			e.ETA = ""
		}
	}
	endOrder := db.Phase(ctx, "status_driver_order")
	err = applyBoardDriverOrder(ctx, tx, &result)
	endOrder()
	if err != nil {
		return result, err
	}
	applyBoardDispatcherOrder(&result)
	if displayBoard {
		result.PendingDrivers, err = readBoardPendingDrivers(ctx, tx)
		if err != nil {
			return result, err
		}
		if pendingViewers != nil && len(result.PendingDrivers) != 0 {
			rows, err := tx.Query(ctx, `SELECT u.id FROM system_task_assignments a JOIN app_users u ON `+systemTaskAssigneePredicate+`
 WHERE a.kind='driver_onboarding'`)
			if err != nil {
				return result, err
			}
			*pendingViewers, err = pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return result, err
			}
		}
	}
	return result, tx.Commit(ctx)
}

type BoardPendingDriver struct {
	IntakeID string `json:"intakeId"`
	FullName string `json:"fullName"`
}

func (r *DriverBoardRepository) CanViewPendingDrivers(ctx context.Context) (bool, error) {
	return systemTaskVisible(ctx, r.pool, "driver_onboarding", false)
}

func readBoardPendingDrivers(ctx context.Context, tx pgx.Tx) ([]BoardPendingDriver, error) {
	result := []BoardPendingDriver{}
	visible, err := systemTaskVisible(ctx, tx, "driver_onboarding", false)
	if err != nil || !visible {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT id,coalesce(driver_data->>'fullName','') FROM fleetscope_driver_intake WHERE completed_at IS NULL AND terminated_at IS NULL ORDER BY lower(driver_data->>'fullName'),id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var d BoardPendingDriver
		if err = rows.Scan(&d.IntakeID, &d.FullName); err != nil {
			return result, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (r *DriverBoardRepository) Save(ctx context.Context, entries []DriverBoardEntry, actor ...string) ([]DriverBoardEntry, error) {
	saved, err := r.saveWithConflict(ctx, entries, actor...)
	if errors.Is(err, ErrDriverBoardConflict) {
		return nil, ErrDriverBoardConflict
	}
	return saved, err
}

func (r *DriverBoardRepository) saveWithConflict(ctx context.Context, entries []DriverBoardEntry, actor ...string) ([]DriverBoardEntry, error) {
	// An explicit equipment restriction updates the assigned truck. Start with
	// the source lock to avoid replaying the command after its guarded rollback.
	for _, e := range entries {
		if e.StatusEdited && (e.Status == "SHOP" || e.Status == "STOP" || e.Status == "ACCIDENT") {
			defer r.workPriority.interactive()()
			return r.save(ctx, entries, true, actor...)
		}
	}
	return runBoardCommand(r.workPriority, func(source bool) ([]DriverBoardEntry, error) { return r.save(ctx, entries, source, actor...) })
}

func (r *DriverBoardRepository) save(ctx context.Context, entries []DriverBoardEntry, source bool, actor ...string) ([]DriverBoardEntry, error) {
	user := ""
	if len(actor) > 0 {
		user = actor[0]
	}
	tx, err := beginBoardMutation(ctx, r.pool, source, user, "board")
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	entries, err = saveDriverBoardEntries(ctx, tx, entries, boardLoadToday(r.now()), true)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if err = attachBoardSaveReceipt(ctx, tx, &entries[i]); err != nil {
			return nil, err
		}
	}
	return entries, tx.Commit(ctx)
}

func saveDriverBoardEntries(ctx context.Context, tx pgx.Tx, entries []DriverBoardEntry, today string, matchCurrentLoad bool) ([]DriverBoardEntry, error) {
	var err error
	entries = append([]DriverBoardEntry(nil), entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].DriverID < entries[j].DriverID })
	for index := range entries {
		e := &entries[index]
		when := ""
		if e.EffectiveAt != nil {
			when = e.EffectiveAt.UTC().Format(time.RFC3339Nano)
		}
		e.UndoID = 0
		// Keep the row read in its own statement after acquiring the driver lock.
		// Send the three statements together to avoid two database round trips.
		var batch pgx.Batch
		batch.Queue(`SELECT set_config('mserp.status_effective_at',$1,true),set_config('mserp.status_reason',$2,true)`, when, strings.TrimSpace(e.StatusReason))
		var active, wasHome bool
		var old DriverBoardEntry
		var readErr error
		batch.Queue(`SELECT active,fleet_status_enabled() AND availability_status='home' FROM drivers WHERE id=$1 FOR UPDATE`, e.DriverID).QueryRow(func(row pgx.Row) error {
			return row.Scan(&active, &wasHome)
		})
		batch.Queue(boardEntryQuery, e.DriverID).QueryRow(func(row pgx.Row) error {
			old, readErr = scanBoardEntry(row)
			return readErr
		})
		err = tx.SendBatch(ctx, &batch).Close()
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDriverBoardConflict
		}
		if err != nil {
			return nil, err
		}
		if matchCurrentLoad && active && e.EditedFields != nil && e.Version <= old.Version {
			empty := len(e.EditedFields) == 0
			e.applyInlineEdit(ctx, old)
			if empty {
				continue
			}
		}
		if matchCurrentLoad && e.DispatchOnly {
			// The dispatcher did not edit these cells. Keep their committed values
			// even when a text-only save landed while this command was in flight.
			e.Notes, e.TrailerNumber, e.TrailerNote = old.Notes, old.TrailerNumber, old.TrailerNote
			if e.Version > 0 && e.Version >= old.loadActionVersion && e.Version < old.Version {
				e.Version = old.Version
				db.Event(ctx, "board_inline_crossed_text_edit")
			}
			e.DispatchOnly = false
		}
		if matchCurrentLoad && active && len(entries) == 1 && old.Version != e.Version {
			return nil, &DriverBoardRowConflict{Entries: []DriverBoardEntry{old}}
		}
		if !active || old.Version != e.Version {
			return nil, ErrDriverBoardConflict
		}
		if e.LeavePlan == nil {
			e.LeavePlan = old.LeavePlan
		}
		if err := e.LeavePlan.Validate(); err != nil {
			return nil, err
		}
		var loadStateValue boardLoadState
		if e.ResolveCurrentLoad || e.StatusEdited || old.CurrentLoad != e.CurrentLoad || old.Destination != e.Destination || old.ETA != e.ETA {
			loadStateValue, _, readErr = loadState(ctx, tx, e.DriverID)
			if readErr != nil {
				return nil, readErr
			}
		}
		if strings.TrimSpace(e.CurrentLoad) != "" &&
			(!strings.EqualFold(strings.TrimSpace(e.CurrentLoad), strings.TrimSpace(old.CurrentLoad)) || (matchCurrentLoad && e.ResolveCurrentLoad)) {
			if matchCurrentLoad {
				explicitStatus := ""
				if e.StatusEdited {
					explicitStatus = e.Status
				}
				_, err = prepareBoardLoadAssignment(ctx, tx, e.DriverID, explicitStatus, true)
			} else {
				err = requireDispatchableTruck(ctx, tx, e.DriverID)
			}
			if err != nil {
				return nil, err
			}
		}
		oldStopKey := loadStateValue.StopKey
		stateChanged := false
		if (old.CurrentLoad != e.CurrentLoad || e.ResolveCurrentLoad) && loadStateValue.Current != nil {
			loadStateValue.Current = nil
			loadStateValue.StopKey = ""
			loadStateValue.DestinationSource = false
			loadStateValue.DestinationManual = false
			stateChanged = true
		}
		if old.Destination != e.Destination && loadStateValue.Current != nil {
			loadStateValue.DestinationSource = strings.TrimSpace(e.Destination) == "" && loadStateValue.StopKey != ""
			loadStateValue.DestinationManual = strings.TrimSpace(e.Destination) != ""
			stateChanged = true
		}
		// Normal text autosave resolves against the whole current week, independent
		// of the date/status filters used by the Next loads queue. Explicit drawer
		// actions and undo restore their own selection without this inference.
		if matchCurrentLoad && (old.CurrentLoad != e.CurrentLoad || e.ResolveCurrentLoad) {
			if !e.StatusEdited {
				e.Status = "DISPATCHED"
				if strings.TrimSpace(e.CurrentLoad) == "" {
					e.Status, err = boardClearedLoadStatus(ctx, tx, e.DriverID)
					if err != nil {
						return nil, err
					}
				}
			}
			if strings.TrimSpace(e.CurrentLoad) == "" || e.Destination == old.Destination {
				e.Destination = ""
			}
			if strings.TrimSpace(e.CurrentLoad) != "" {
				chosen, err := selectBoardCurrentLoad(ctx, tx, e.DriverID, today, e.CurrentLoad)
				if err != nil {
					return nil, err
				}
				if chosen != nil {
					if providerCompletedLoad(chosen.SourceStatus) {
						return nil, ErrBoardLoadCompleted
					}
					loadStateValue.Current = chosen
					loadStateValue.StopKey = ""
					loadStateValue.DestinationSource = false
					loadStateValue.DestinationManual = e.Destination != ""
					if stop := defaultBoardStop(chosen.Stops); stop != nil && e.Destination == "" {
						loadStateValue.StopKey = stop.Key
						loadStateValue.DestinationSource = true
					}
					if (e.Status == "ENROUTE" || e.Status == "RESERVED") && e.Destination == "" {
						useBoardStop(&loadStateValue, "delivery")
					}
					stateChanged = true
				}
			}
		}
		if matchCurrentLoad && !e.ResolveCurrentLoad && old.CurrentLoad == e.CurrentLoad && e.StatusEdited && loadStateValue.Current != nil {
			if e.Status == "DISPATCHED" {
				useBoardStop(&loadStateValue, "pickup")
				e.Destination = ""
				stateChanged = true
			}
			if e.Status == "ENROUTE" || e.Status == "RESERVED" {
				useBoardStop(&loadStateValue, "delivery")
				e.Destination = ""
				stateChanged = true
			}
		}
		e.ResolveCurrentLoad = false
		if old.CurrentLoad != e.CurrentLoad || old.Destination != e.Destination || oldStopKey != loadStateValue.StopKey {
			if loadStateValue.AutomationContext != "" || loadStateValue.ArrivalOverride != "" || loadStateValue.TravelMode != "" || (old.CurrentLoad != e.CurrentLoad && loadStateValue.ConfirmedLoadID != nil) {
				stateChanged = true
			}
			if e.ETA == old.ETA {
				e.ETA = ""
			}
			loadStateValue.AutomationContext = ""
			loadStateValue.ArrivalOverride = ""
			loadStateValue.TravelMode = ""
			if old.CurrentLoad != e.CurrentLoad {
				loadStateValue.ConfirmedLoadID = nil
			}
		}
		if old.ETA != e.ETA && e.ETA != "" {
			loadStateValue.TravelMode = "manual"
			stateChanged = true
		}
		e.StatusEdited = false
		var version int
		if e.Version == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO driver_board(driver_id,current_load,trailer_number,status,destination,eta,notes,home_time,trailer_note,leave_plan)
   VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
   ON CONFLICT(driver_id) DO NOTHING RETURNING version`, e.DriverID, e.CurrentLoad, e.TrailerNumber, e.Status, e.Destination, e.ETA, e.Notes, e.HomeTime, e.TrailerNote, e.LeavePlan).Scan(&version)
		} else {
			err = tx.QueryRow(ctx, `UPDATE driver_board SET current_load=$2,trailer_number=$3,status=$4,destination=$5,eta=$6,notes=$7,home_time=$8,trailer_note=$10,leave_plan=$11,
    version=version+1,updated_at=now() WHERE driver_id=$1 AND version=$9 RETURNING version`,
				e.DriverID, e.CurrentLoad, e.TrailerNumber, e.Status, e.Destination, e.ETA, e.Notes, e.HomeTime, e.Version, e.TrailerNote, e.LeavePlan).Scan(&version)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDriverBoardConflict
		}
		if err != nil {
			return nil, err
		}
		e.Version = version
		if err = tx.QueryRow(ctx, `SELECT status,version FROM driver_board WHERE driver_id=$1`, e.DriverID).Scan(&e.Status, &e.Version); err != nil {
			return nil, err
		}
		if today != "" && (e.HomeTime != "" || (e.LeavePlan != nil && e.LeavePlan.HomeStart != "")) {
			var stillHome bool
			if err = tx.QueryRow(ctx, `SELECT CASE WHEN fleet_status_enabled() THEN availability_status='home' ELSE $2 IN ('HOME','HOME (YARD)') END FROM drivers WHERE id=$1`, e.DriverID, e.Status).Scan(&stillHome); err != nil {
				return nil, err
			}
			if !stillHome && e.clearReturnedHomeTime(wasHome || old.Status == "HOME" || old.Status == "HOME (YARD)", today) {
				if err = tx.QueryRow(ctx, `UPDATE driver_board SET home_time=$2,leave_plan=$3,version=version+1,updated_at=now() WHERE driver_id=$1 RETURNING version`, e.DriverID, e.HomeTime, e.LeavePlan).Scan(&e.Version); err != nil {
					return nil, err
				}
			}
		}
		e.EffectiveAt = nil
		e.StatusReason = ""
		if stateChanged {
			if err = saveLoadState(ctx, tx, e.DriverID, loadStateValue); err != nil {
				return nil, err
			}
		}
		if matchCurrentLoad && (old.ETA != e.ETA || old.CurrentLoad != e.CurrentLoad || stateChanged) {
			views, readErr := readBoardLoads(ctx, tx, []string{e.DriverID}, today, time.Now())
			if readErr != nil {
				return nil, readErr
			}
			if a := views[e.DriverID].Automation; a != nil && (old.ETA != e.ETA || old.CurrentLoad != e.CurrentLoad || e.ETA == "") {
				if _, err = tx.Exec(ctx, `INSERT INTO driver_board_automation(driver_id,eta_context) VALUES($1,$2) ON CONFLICT(driver_id) DO UPDATE SET eta_context=EXCLUDED.eta_context`, e.DriverID, a.Context); err != nil {
					return nil, err
				}
			}
		}
	}
	return entries, nil
}
