package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/datatruck"
)

var ErrBoardLoadSelection = errors.New("load plans changed or the selection is unavailable; refresh Loads and review the current plans")
var ErrBoardLoadCompleted = errors.New("a load completed in DataTruck cannot be made current")
var ErrBoardGrossPlanRemoval = errors.New("remove this load from Gross Board; Gross Board plans cannot be removed from the Status Board queue")

type BoardStop struct {
	ManualStopTime string         `json:"manualStopTime,omitempty"`
	ProviderID     int            `json:"providerId,omitempty"`
	Latitude       *float64       `json:"latitude,omitempty"`
	Longitude      *float64       `json:"longitude,omitempty"`
	Schedule       *BoardSchedule `json:"schedule,omitempty"`
	Key            string         `json:"key"`
	Type           string         `json:"type"`
	Location       string         `json:"location"`
	StreetAddress  string         `json:"streetAddress,omitempty"`
	Appointment    string         `json:"appointment"`
}
type BoardLoad struct {
	queueAlias          string
	PreviouslyActive    bool `json:"previouslyActive,omitempty"`
	previousActiveEvent int64
	CanRemoveFromQueue  bool        `json:"canRemoveFromQueue,omitempty"`
	Overdue             bool        `json:"overdue,omitempty"`
	Completed           bool        `json:"completed,omitempty"`
	CanRestoreToQueue   bool        `json:"canRestoreToQueue,omitempty"`
	Manual              bool        `json:"manual,omitempty"`
	PlanID              string      `json:"planId"`
	Date                string      `json:"date"`
	Slot                int         `json:"slot"`
	Number              string      `json:"number"`
	LoadID              *int        `json:"loadId"`
	SourceStatus        string      `json:"sourceStatus"`
	SourceDriver        string      `json:"sourceDriver"`
	SourceMissing       bool        `json:"sourceMissing,omitempty"`
	SyncedAt            string      `json:"syncedAt"`
	Stops               []BoardStop `json:"stops"`
	Warning             string      `json:"warning"`
}
type boardLoadState struct {
	RequeuedLoads         map[string]int64  `json:"requeuedLoads,omitempty"`
	Manual                []BoardLoad       `json:"manual,omitempty"`
	ManualStopTimes       map[string]string `json:"manualStopTimes,omitempty"`
	AutomationContext     string            `json:"automationContext,omitempty"`
	ArrivalOverride       string            `json:"arrivalOverride,omitempty"`
	TravelMode            string            `json:"travelMode,omitempty"`
	ConfirmedLoadID       *int              `json:"confirmedLoadId,omitempty"`
	ConfirmedSourceDriver string            `json:"confirmedSourceDriver,omitempty"`
	Current               *BoardLoad        `json:"current"`
	Order                 []string          `json:"order"`
	OrderLabels           []string          `json:"orderLabels"`
	Hidden                []string          `json:"hidden"`
	DestinationSource     bool              `json:"destinationSource"`
	DestinationManual     bool              `json:"destinationManual,omitempty"`
	StopKey               string            `json:"stopKey"`
	Completed             []string          `json:"completed,omitempty"`
	CompletedStops        map[string]bool   `json:"completedStops,omitempty"`
	RestoredPlans         map[string]bool   `json:"restoredPlans,omitempty"`
}

func boardCompletionKey(p BoardLoad) string {
	if p.LoadID != nil {
		return fmt.Sprintf("load:%d", *p.LoadID)
	}
	return "plan:" + p.PlanID
}

func boardProgressStatus(status string, next []BoardLoad) string {
	if status == "ENROUTE" || status == "RESERVED" {
		if len(next) > 0 {
			return "RESERVED"
		}
		return "ENROUTE"
	}
	return status
}

func useBoardStop(s *boardLoadState, kind string) {
	s.StopKey = ""
	s.DestinationSource = false
	s.DestinationManual = false
	if s.Current == nil {
		return
	}
	for _, stop := range s.Current.Stops {
		if strings.EqualFold(stop.Type, kind) && strings.TrimSpace(stop.Location) != "" {
			s.StopKey, s.DestinationSource = stop.Key, true
			return
		}
	}
}

// A selected plan may have had no stops when it was saved. Once its source
// arrives, project the appropriate location without changing dispatch progress,
// creating history on reads, or replacing an explicit manual destination.
func lateBoardStop(s boardLoadState, current BoardLoad, status, destination string) *BoardStop {
	if s.Current == nil || s.Current.PlanID != current.PlanID || s.DestinationManual ||
		s.DestinationSource || s.StopKey != "" || destination != "" || len(s.Current.Stops) != 0 ||
		current.LoadID == nil || (s.Current.LoadID != nil && *s.Current.LoadID != *current.LoadID) {
		return nil
	}
	kind := "pickup"
	if status == "ENROUTE" || status == "RESERVED" {
		kind = "delivery"
	} else if status != "DISPATCHED" {
		return nil
	}
	for _, stop := range current.Stops {
		if strings.EqualFold(stop.Type, kind) && strings.TrimSpace(stop.Location) != "" {
			return &stop
		}
	}
	return nil
}

type BoardLoads struct {
	CompletedStopKeys []string         `json:"completedStopKeys,omitempty"`
	DetailsOmitted    bool             `json:"detailsOmitted,omitempty"`
	ResolutionWarning string           `json:"resolutionWarning,omitempty"`
	Automation        *BoardAutomation `json:"automation,omitempty"`
	Current           *BoardLoad       `json:"current"`
	Week              []BoardLoad      `json:"week"`
	Next              []BoardLoad      `json:"next"`
	Earlier           []BoardLoad      `json:"earlier"`
	Hidden            []BoardLoad      `json:"hidden"`
	Unavailable       []BoardLoad      `json:"unavailable"`
	DestinationSource bool             `json:"destinationSource"`
	SourceDestination string           `json:"sourceDestination"`
	StopKey           string           `json:"stopKey"`
	FromDate          string           `json:"fromDate"`
	Revision          string           `json:"revision"`
	CustomOrder       bool             `json:"customOrder"`
}
type BoardLoadAction struct {
	Number         string   `json:"number"`
	DriverID       string   `json:"driverId"`
	Version        int      `json:"version"`
	FromDate       string   `json:"fromDate"`
	Revision       string   `json:"revision"`
	Action         string   `json:"action"`
	PlanID         string   `json:"planId"`
	Order          []string `json:"order"`
	StopKey        string   `json:"stopKey"`
	ManualStopTime string   `json:"manualStopTime"`
}
type BoardLoadResult struct {
	Entry DriverBoardEntry `json:"entry"`
	Loads BoardLoads       `json:"loads"`
}

// Status Board number-only plans resolve against available source records. Historical
// explicit IDs stay pinned, including deleted sources, so dispatch is never retargeted.
const boardResolvedLoad = `
 LEFT JOIN LATERAL (
   SELECT min(l.id) AS id FROM loads l
   WHERE NOT e.deleted AND e.day_status='' AND ((e.load_record_id IS NOT NULL AND l.id=e.load_record_id)
      OR (e.load_record_id IS NULL AND e.load_number<>'' AND lower(btrim(l.load_id))=lower(btrim(e.load_number))
          AND ` + boardAvailableSourceSQL + `))
   HAVING count(*)=1
 ) matched ON true
 LEFT JOIN loads l ON l.id=matched.id `

const boardPlansSQL = `(SELECT driver_id,service_date,0 AS slot,false AS deleted,load_number,load_record_id,day_status,plan_id FROM gross_board_entries
 UNION ALL SELECT driver_id,service_date,slot,deleted,load_number,load_record_id,day_status,plan_id FROM gross_board_extra_entries)`

func boardLoadToday(now time.Time) string {
	loc, _ := time.LoadLocation("America/New_York")
	return now.In(loc).Format(time.DateOnly)
}

// Gross Board and queue windows use New York calendar dates. FromDate stays
// today for stale editor/midnight fencing, separately from queue eligibility.
func boardLoadWeekStart(from string) string {
	day, _ := time.Parse(time.DateOnly, from)
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7).Format(time.DateOnly)
}

// Carry the previous Saturday/Sunday through the whole current week. Older
// plans remain inspectable in Earlier unless deliberately restored.
func boardLoadQueueStart(from string) string {
	week, _ := time.Parse(time.DateOnly, boardLoadWeekStart(from))
	return week.AddDate(0, 0, -2).Format(time.DateOnly)
}

func boardPlanQueued(p BoardLoad, s boardLoadState, from string) bool {
	return !p.PreviouslyActive && (boardLoadInTransit(p) || p.Manual || p.Date >= boardLoadQueueStart(from) || s.RestoredPlans[p.PlanID])
}

// System loads need an actual pickup deadline, never a Gross Board date or
// pickup-start guess. Local-only schedules cannot establish an instant without
// a facility timezone. Free-text overrides are likewise deliberately unknown.
func boardLoadOverdue(p BoardLoad, from string, now time.Time) bool {
	if p.Manual || boardLoadInTransit(p) {
		return false
	}
	if p.LoadID == nil {
		return p.Date < from
	}
	for _, stop := range p.Stops {
		if stop.Type != "pickup" {
			continue
		}
		schedule := stop.Schedule
		if schedule == nil || (schedule.Kind != "window" && schedule.Kind != "appointment") {
			return false
		}
		end, err := time.Parse(time.RFC3339, schedule.EndAt)
		if err != nil {
			return false
		}
		if schedule.BeginAt != "" {
			begin, err := time.Parse(time.RFC3339, schedule.BeginAt)
			if err != nil || end.Before(begin) {
				return false
			}
		}
		return now.After(end)
	}
	return false
}

func boardLoadInTransit(p BoardLoad) bool {
	// A source still in transit remains active, even after its planned date.
	// Only explicit completion or other existing history safeguards retire it.
	// A driver's dispatch status says nothing about other queued plans.
	if p.LoadID != nil {
		switch strings.ToLower(strings.TrimSpace(p.SourceStatus)) {
		case "in_transit", "in transit", "in-transit":
			return true
		}
	}
	return false
}

func (r *DriverBoardRepository) loadStartDate(from string) string {
	today := boardLoadToday(r.now())
	if from < today {
		return today
	}
	return from
}

func loadState(ctx context.Context, tx pgx.Tx, id string) (boardLoadState, string, error) {
	var s boardLoadState
	var raw string
	err := tx.QueryRow(ctx, `SELECT payload::text FROM driver_board_load_state WHERE driver_id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, "{}", nil
	}
	if err != nil {
		return s, "", err
	}
	err = json.Unmarshal([]byte(raw), &s)
	return s, raw, err
}
func saveLoadState(ctx context.Context, tx pgx.Tx, id string, s boardLoadState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,$2::jsonb)
 ON CONFLICT(driver_id) DO UPDATE SET payload=EXCLUDED.payload`, id, raw)
	return err
}
func boardStops(raw []byte) []BoardStop {
	result := []BoardStop{}
	var load datatruck.Load
	if json.Unmarshal(raw, &load) != nil {
		return result
	}
	sort.SliceStable(load.Stops, func(i, j int) bool { return load.Stops[i].Ordering < load.Stops[j].Ordering })
	lastDelivery := -1
	firstPickup := -1
	for i, s := range load.Stops {
		if s.StopType == "pickup" && firstPickup < 0 {
			firstPickup = i
		}
		if s.StopType == "delivery" {
			lastDelivery = i
		}
	}
	for i, s := range load.Stops {
		parts := []string{}
		for _, v := range []string{s.Location.City, s.Location.State, s.Location.ZipCode} {
			if strings.TrimSpace(v) != "" {
				parts = append(parts, strings.TrimSpace(v))
			}
		}
		location := strings.Join(parts, ", ")
		streetAddress := strings.TrimSpace(s.Location.Address)
		if location == "" {
			location = streetAddress
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s/%s", s.Ordering, s.StopType, location, s.Location.Address)))
		stop := BoardStop{Key: hex.EncodeToString(hash[:12]), Type: s.StopType, Location: location}
		if streetAddress != location {
			stop.StreetAddress = streetAddress
		}
		stop.ProviderID = s.ID
		stop.Schedule = boardSchedule(s)
		stop.Latitude, stop.Longitude = boardStopCoordinates(s)
		// Only load-level appointments are known: never assign them to intermediate stops.
		if i == firstPickup && load.PickupAppointmentTime != nil {
			stop.Appointment = load.PickupAppointmentTime.Format(time.RFC3339)
		}
		if i == lastDelivery && load.DeliveryAppointmentTime != nil {
			stop.Appointment = load.DeliveryAppointmentTime.Format(time.RFC3339)
		}
		result = append(result, stop)
	}
	return result
}
func providerCompletedLoad(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "delivered", "completed", "invoiced":
		return true
	}
	return false
}

func terminalLoad(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled":
		return true
	}
	return providerCompletedLoad(status)
}
func annotateLoad(p *BoardLoad, driver string) {
	if p.SourceMissing {
		p.Warning = "DataTruck no longer returns this source record; pickup and delivery details are unavailable"
		p.Stops = []BoardStop{}
		return
	}
	if p.LoadID == nil {
		p.Warning = "Unmatched plan"
		if p.Manual {
			p.Warning = "Load details are not available yet or the number is ambiguous. Details will appear when a unique matching load is in the system."
		}
	}
	if terminalLoad(p.SourceStatus) {
		p.Warning = "DataTruck reports " + p.SourceStatus + "; review before replacing the current load"
	}
}

// Repeated slots for the same imported load are equivalent; distinct matching
// loads (or unresolved duplicate plans) must never be guessed from their number.
func matchBoardCurrentLoad(plans []BoardLoad, number string) *BoardLoad {
	var chosen *BoardLoad
	for _, p := range plans {
		if !strings.EqualFold(strings.TrimSpace(p.Number), strings.TrimSpace(number)) {
			continue
		}
		if chosen != nil {
			if chosen.LoadID == nil || p.LoadID == nil || *chosen.LoadID != *p.LoadID {
				return nil
			}
			continue
		}
		v := p
		chosen = &v
	}
	return chosen
}

func hasBoardNumber(plans []BoardLoad, number string) bool {
	for _, p := range plans {
		if strings.EqualFold(strings.TrimSpace(p.Number), strings.TrimSpace(number)) {
			return true
		}
	}
	return false
}

// A dispatch selection keeps its identity when accounting adds or recreates a
// plan for the same imported load. Only reconnect an absent plan when every
// exact-number candidate resolves to the already selected source record.
// Preserve the selection's plan/manual identity for queue actions and undo.
func refreshedBoardCurrent(saved BoardLoad, plans []BoardLoad) BoardLoad {
	current := saved
	current.Warning = "Current plan was removed, replaced or reassigned in Gross Board; review it"
	if saved.PlanID != "" {
		for _, p := range plans {
			if p.PlanID != saved.PlanID {
				continue
			}
			if saved.LoadID != nil && (p.LoadID == nil || *saved.LoadID != *p.LoadID) {
				current.Warning = "Current plan's load match changed; review it"
				return current
			}
			return p
		}
	}
	if saved.LoadID != nil {
		if p := matchBoardCurrentLoad(plans, saved.Number); p != nil && p.LoadID != nil && *p.LoadID == *saved.LoadID {
			current = *p
			current.PlanID, current.Manual = saved.PlanID, saved.Manual
		}
	}
	return current
}

func defaultBoardStop(stops []BoardStop) *BoardStop {
	// New current loads always start at pickup.
	for i := range stops {
		if strings.EqualFold(stops[i].Type, "pickup") && strings.TrimSpace(stops[i].Location) != "" {
			return &stops[i]
		}
	}
	return nil
}

// Source sync timestamps are freshness metadata. A routine refresh must not
// invalidate an open editor when the actual load plans/stops are unchanged.
// Copy the view to preserve those timestamps in API responses and saved details.
func boardLoadsRevision(view BoardLoads, state boardLoadState, version int) string {
	withoutSync := func(load BoardLoad) BoardLoad {
		load.SyncedAt = ""
		return load
	}
	list := func(loads []BoardLoad) []BoardLoad {
		if loads == nil {
			return nil
		}
		out := make([]BoardLoad, len(loads))
		for i, load := range loads {
			out[i] = withoutSync(load)
		}
		return out
	}
	if view.Current != nil {
		current := withoutSync(*view.Current)
		view.Current = &current
	}
	view.Week, view.Next, view.Earlier = list(view.Week), list(view.Next), list(view.Earlier)
	view.Hidden, view.Unavailable = list(view.Hidden), list(view.Unavailable)
	raw, _ := json.Marshal(struct {
		View    BoardLoads
		State   boardLoadState
		Version int
	}{view, state, version})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Read-only reconciliation: no calendar or upstream status can promote a load.
func readBoardLoads(ctx context.Context, tx pgx.Tx, ids []string, from string, now time.Time) (map[string]BoardLoads, error) {
	if _, err := tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
		return nil, err
	}
	day, _ := time.Parse(time.DateOnly, from)
	week := boardLoadWeekStart(from)
	weekEnd := day.AddDate(0, 0, 7-(int(day.Weekday())+6)%7).Format(time.DateOnly)
	result := map[string]BoardLoads{}
	states := map[string]boardLoadState{}
	manualMismatch := map[string]bool{}
	entries := map[string]DriverBoardEntry{}
	selected := []string{}
	for _, id := range ids {
		states[id] = boardLoadState{}
		result[id] = BoardLoads{Week: []BoardLoad{}, Next: []BoardLoad{}, Earlier: []BoardLoad{}, Hidden: []BoardLoad{}, Unavailable: []BoardLoad{}, FromDate: from}
	}
	rows, err := tx.Query(ctx, `SELECT b.driver_id,coalesce(s.payload,'{}'::jsonb),coalesce(b.current_load,''),coalesce(b.status,''),coalesce(b.destination,''),b.version FROM driver_board b LEFT JOIN driver_board_load_state s ON b.driver_id=s.driver_id WHERE b.driver_id=ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var s boardLoadState
		var currentText string
		var entry DriverBoardEntry
		if err = rows.Scan(&id, &s, &currentText, &entry.Status, &entry.Destination, &entry.Version); err != nil {
			rows.Close()
			return nil, err
		}
		states[id] = s
		entry.CurrentLoad = currentText
		entries[id] = entry
		if s.Current != nil {
			if s.Current.PlanID != "" {
				selected = append(selected, s.Current.PlanID)
			}
			manualMismatch[id] = !strings.EqualFold(strings.TrimSpace(currentText), strings.TrimSpace(s.Current.Number))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	plans := map[string][]BoardLoad{}
	rows, err = tx.Query(ctx, `SELECT e.driver_id,e.plan_id,e.service_date::text,e.slot,e.load_number,l.id,coalesce(l.status,''),e.driver_id::text,coalesce(l.synced_at::text,''),coalesce(l.raw_payload,'{}'::jsonb),NOT (`+boardAvailableSourceSQL+`)
 FROM `+boardPlansSQL+` e `+boardResolvedLoad+`
 LEFT JOIN driver_board_load_state current_state ON current_state.driver_id=e.driver_id
 WHERE e.driver_id=ANY($1::uuid[]) AND NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''
 AND (e.service_date >= $2::date OR e.plan_id=ANY($3::uuid[]) OR l.id::text=current_state.payload->'current'->>'loadId'
 OR lower(btrim(coalesce(l.status,''))) NOT IN ('delivered','completed','invoiced','cancelled','canceled')
 OR coalesce((SELECT payload->'order' ? e.plan_id::text FROM driver_board_load_state WHERE driver_id=e.driver_id),false))
 ORDER BY e.driver_id,e.service_date,e.slot`, ids, week, selected)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var p BoardLoad
		var raw []byte
		if err = rows.Scan(&id, &p.PlanID, &p.Date, &p.Slot, &p.Number, &p.LoadID, &p.SourceStatus, &p.SourceDriver, &p.SyncedAt, &raw, &p.SourceMissing); err != nil {
			rows.Close()
			return nil, err
		}
		p.Stops = boardStops(raw)
		annotateLoad(&p, id)
		plans[id] = append(plans[id], p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	manualPlans, err := readManualBoardLoads(ctx, tx, states)
	if err != nil {
		return nil, err
	}
	previouslyActive, err := readPreviouslyActiveBoardLoads(ctx, tx, plans, manualPlans)
	if err != nil {
		return nil, err
	}
	// A saved selection may outlive its plan. Recheck its exact source ID;
	// never silently switch a pinned selection to a recreated order.
	missingIDs := map[int]bool{}
	missingRows, err := tx.Query(ctx, `SELECT load_id FROM load_source_availability WHERE NOT present`)
	if err != nil {
		return nil, err
	}
	for missingRows.Next() {
		var id int
		if err := missingRows.Scan(&id); err != nil {
			missingRows.Close()
			return nil, err
		}
		missingIDs[id] = true
	}
	err = missingRows.Err()
	missingRows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		s := states[id]
		view := result[id]
		// Manual queue identities survive later imports and Gross Board planning.
		// Keep the real Gross Board plans in Week, but show each source once in the queue.
		plans[id] = append(manualPlans[id], plans[id]...)
		// Unplanned current loads are source lookups, never synthetic plans.
		if strings.TrimSpace(entries[id].CurrentLoad) != "" && (s.Current == nil || s.Current.PlanID == "") && !hasBoardNumber(plans[id], entries[id].CurrentLoad) {
			p, e := importedBoardLoad(ctx, tx, entries[id].CurrentLoad)
			if e != nil {
				return nil, e
			}
			if p != nil {
				if s.Current == nil {
					s.Current = p
				}
				if s.StopKey == "" && !s.DestinationManual && entries[id].Destination == "" {
					kind := "pickup"
					if entries[id].Status == "ENROUTE" || entries[id].Status == "RESERVED" {
						kind = "delivery"
					}
					useBoardStop(&s, kind)
				}
				plans[id] = append(plans[id], *p)
			} else if s.Current == nil {
				s.StopKey = ""
				s.DestinationSource = false
			}
		}
		for _, p := range plans[id] {
			if !p.Manual && p.PlanID != "" && p.Date >= week && p.Date < weekEnd {
				p.Completed = !p.SourceMissing && providerCompletedLoad(p.SourceStatus)
				view.Week = append(view.Week, p)
			}
		}
		view.DestinationSource = s.DestinationSource
		view.StopKey = s.StopKey
		view.CustomOrder = len(s.Order) > 0
		if s.Current != nil {
			current := refreshedBoardCurrent(*s.Current, plans[id])
			if current.LoadID != nil && missingIDs[*current.LoadID] {
				current.SourceMissing = true
				annotateLoad(&current, id)
				s.DestinationSource = false
				view.DestinationSource, view.StopKey = false, ""
			}
			view.Current = &current
			if !manualMismatch[id] {
				if stop := lateBoardStop(s, current, entries[id].Status, entries[id].Destination); stop != nil {
					view.StopKey, view.DestinationSource = stop.Key, true
				}
			}
			if manualMismatch[id] {
				view.Current.Warning = "Current load text differs from the selected plan; clear or reselect the load"
				view.DestinationSource = false
			}
			for _, stop := range current.Stops {
				if s.CompletedStops[boardCompletionKey(current)+"/"+stop.Key] {
					view.CompletedStopKeys = append(view.CompletedStopKeys, stop.Key)
				}
				if stop.Key == view.StopKey {
					view.SourceDestination = stop.Location
				}
			}
			if view.DestinationSource && view.SourceDestination == "" {
				view.Current.Warning = strings.TrimSpace(view.Current.Warning + " Selected stop is missing or has no location; select a stop or use a manual destination")
			}
		}
		seen := map[int]bool{}
		aliases := manualBoardPlanAliases(plans[id])
		seenPlans := map[string]bool{}
		if view.Current != nil {
			view.Current.queueAlias = aliases[view.Current.PlanID]
			if key := view.Current.queueAlias; key != "" {
				seenPlans[key] = true
			}
		}
		if view.Current == nil && strings.TrimSpace(entries[id].CurrentLoad) != "" {
			view.ResolutionWarning = "Current load is unmatched or ambiguous; review its exact number and load plans"
		}
		if view.Current != nil && view.Current.LoadID != nil {
			seen[*view.Current.LoadID] = true
		}
		hidden := map[string]bool{}
		for _, k := range s.Hidden {
			hidden[k] = true
		}
		completed := map[string]bool{}
		for _, key := range s.Completed {
			completed[key] = true
		}
		// An older duplicate must not hide the same source's eligible plan.
		// Prefer eligible Gross Board counterparts, preserving date/slot order.
		queuePlans := slices.Clone(plans[id])
		for i := range queuePlans {
			annotatePreviouslyActive(&queuePlans[i], previouslyActive[id], s)
		}
		preferredGross := preferredGrossBoardQueuePlans(queuePlans, aliases, s, from)
		sort.SliceStable(queuePlans, func(i, j int) bool {
			a, b := queuePlans[i], queuePlans[j]
			ap, bp := preferredGross[aliases[a.PlanID]] == a.PlanID, preferredGross[aliases[b.PlanID]] == b.PlanID
			if ap != bp {
				return ap
			}
			if a.Manual != b.Manual {
				return a.Manual
			}
			return boardPlanQueued(a, s, from) && !boardPlanQueued(b, s, from)
		})
		for _, p := range queuePlans {
			if p.PlanID == "" {
				continue
			}
			if view.Current != nil && p.PlanID == view.Current.PlanID {
				continue
			}
			// A removed manual alias must not suppress its Gross Board counterpart.
			p.CanRemoveFromQueue = p.Manual
			if p.Manual && hidden[p.PlanID] {
				view.Hidden = append(view.Hidden, p)
				continue
			}
			p.queueAlias = aliases[p.PlanID]
			if key := p.queueAlias; key != "" {
				if seenPlans[key] {
					continue
				}
				seenPlans[key] = true
			}
			if p.LoadID != nil {
				if seen[*p.LoadID] {
					continue
				}
				seen[*p.LoadID] = true
			}
			// Gross Board owns the plan. Losing its DataTruck details does not
			// remove an otherwise eligible upcoming load from the queue.
			sourceUnavailable := terminalLoad(p.SourceStatus) || (p.Manual && p.SourceMissing)
			completedHere := completed[boardCompletionKey(p)] || completed["plan:"+p.PlanID] || completed["plan:"+aliases[p.PlanID]]
			if sourceUnavailable || completedHere {
				p.Completed = providerCompletedLoad(p.SourceStatus) || (completedHere && !sourceUnavailable)
				p.PreviouslyActive = false
				if p.Completed && sourceUnavailable {
					p.Warning = ""
				}
				p.CanRestoreToQueue = completedHere && !sourceUnavailable &&
					!strings.EqualFold(strings.TrimSpace(entries[id].CurrentLoad), strings.TrimSpace(p.Number))
				view.Unavailable = append(view.Unavailable, p)
			} else if !boardPlanQueued(p, s, from) {
				p.CanRestoreToQueue = p.PreviouslyActive && !strings.EqualFold(strings.TrimSpace(entries[id].CurrentLoad), strings.TrimSpace(p.Number))
				view.Earlier = append(view.Earlier, p)
			} else {
				p.Overdue = boardLoadOverdue(boardStopTimes(p, s.ManualStopTimes), from, now)
				view.Next = append(view.Next, p)
			}
		}
		rank := map[string]int{}
		for i, key := range s.Order {
			rank[key] = i + 1
		}
		for _, p := range view.Next {
			if rank[p.PlanID] == 0 && aliases[p.PlanID] != "" {
				rank[p.PlanID] = rank[aliases[p.PlanID]]
			}
		}
		sort.SliceStable(view.Next, func(i, j int) bool {
			a, b := rank[view.Next[i].PlanID], rank[view.Next[j].PlanID]
			if a == 0 {
				return false
			}
			if b == 0 {
				return true
			}
			return a < b
		})
		view.Revision = boardLoadsRevision(view, states[id], entries[id].Version)
		for i := range view.Next {
			view.Next[i] = boardStopTimes(view.Next[i], s.ManualStopTimes)
		}
		result[id] = view
	}
	if err := enrichBoardAutomation(ctx, tx, result, states, entries); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *DriverBoardRepository) Loads(ctx context.Context, id, from string) (BoardLoads, error) {
	from = r.loadStartDate(from)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return BoardLoads{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
		return BoardLoads{}, err
	}
	views, err := readBoardLoads(ctx, tx, []string{id}, from, r.now())
	if err != nil {
		return BoardLoads{}, err
	}
	return views[id], tx.Commit(ctx)
}
func boardEntry(ctx context.Context, tx pgx.Tx, id string) (DriverBoardEntry, error) {
	var e DriverBoardEntry
	err := tx.QueryRow(ctx, `SELECT d.id,coalesce(b.current_load,''),coalesce(b.trailer_number,''),coalesce(b.trailer_note,''),coalesce(b.status,''),coalesce(b.destination,''),coalesce(b.eta,''),coalesce(b.notes,''),coalesce(b.home_time,''),coalesce(b.version,0),coalesce(b.leave_plan,'{}'::jsonb)
 FROM drivers d LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.id=$1`, id).Scan(&e.DriverID, &e.CurrentLoad, &e.TrailerNumber, &e.TrailerNote, &e.Status, &e.Destination, &e.ETA, &e.Notes, &e.HomeTime, &e.Version, &e.LeavePlan)
	return e, err
}
func (r *DriverBoardRepository) ChangeLoads(ctx context.Context, a BoardLoadAction, actor string) (BoardLoadResult, error) {
	return runBoardCommand(func(source bool) (BoardLoadResult, error) { return r.changeLoads(ctx, a, actor, source) })
}

func (r *DriverBoardRepository) changeLoads(ctx context.Context, a BoardLoadAction, actor string, source bool) (BoardLoadResult, error) {
	var result BoardLoadResult
	tx, err := beginBoardMutation(ctx, r.pool, source)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var active bool
	if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1 FOR UPDATE`, a.DriverID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
		return result, ErrDriverBoardConflict
	} else if err != nil {
		return result, err
	}
	e, err := boardEntry(ctx, tx, a.DriverID)
	if err != nil {
		return result, err
	}
	if !active || e.Version != a.Version {
		return result, ErrDriverBoardConflict
	}
	a.FromDate = r.loadStartDate(a.FromDate)
	views, err := readBoardLoads(ctx, tx, []string{a.DriverID}, a.FromDate, r.now())
	if err != nil {
		return result, err
	}
	view := views[a.DriverID]
	if view.Revision != a.Revision {
		return result, ErrBoardLoadSelection
	}
	s, _, err := loadState(ctx, tx, a.DriverID)
	if err != nil {
		return result, err
	}
	assigning := a.Action == "add" || a.Action == "select" || a.Action == "restore" || a.Action == "restore_previous"
	switch a.Action {
	case "add":
		number := strings.TrimSpace(a.Number)
		if number == "" || len([]rune(number)) > 100 || strings.ContainsAny(number, "\r\n\t") || len(s.Manual) >= 1000 {
			return result, ErrBoardLoadSelection
		}
		for _, list := range [][]BoardLoad{view.Next, view.Hidden, view.Unavailable, s.Manual} {
			if hasBoardNumber(list, number) {
				return result, ErrBoardLoadSelection
			}
		}
		for _, p := range view.Earlier {
			if (p.PreviouslyActive || boardLoadInTransit(p)) && strings.EqualFold(strings.TrimSpace(p.Number), number) {
				return result, ErrBoardLoadSelection
			}
		}
		if strings.EqualFold(strings.TrimSpace(e.CurrentLoad), number) || (view.Current != nil && strings.EqualFold(strings.TrimSpace(view.Current.Number), number)) {
			return result, ErrBoardLoadSelection
		}
		var planID string
		if err = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&planID); err != nil {
			return result, err
		}
		s.Manual = append(s.Manual, BoardLoad{Manual: true, PlanID: planID, Number: number, Stops: []BoardStop{}})
		// Preserve the visible queue and append the entered load at its end.
		s.Order, s.OrderLabels = nil, nil
		for _, p := range view.Next {
			s.Order = append(s.Order, p.PlanID)
			s.OrderLabels = append(s.OrderLabels, p.Number)
		}
		s.Order = append(s.Order, planID)
		s.OrderLabels = append(s.OrderLabels, number)
	case "stop_time":
		target, stopKey := view.Current, view.StopKey
		if a.PlanID != "" {
			// Only the visible first queued pickup is editable here. Explicit plan
			// and stop keys prevent a reordered queue from editing another load.
			if len(view.Next) == 0 || view.Next[0].PlanID != a.PlanID {
				return result, ErrBoardLoadSelection
			}
			target, stopKey = &view.Next[0], a.StopKey
			for _, stop := range target.Stops {
				if stop.Type == "pickup" {
					if stop.Key != stopKey {
						return result, ErrBoardLoadSelection
					}
					break
				}
			}
		} else if view.Automation == nil || view.Automation.StopType == "" {
			return result, ErrBoardLoadSelection
		}
		if target == nil || target.LoadID == nil || stopKey == "" {
			return result, ErrBoardLoadSelection
		}
		found := false
		for _, stop := range target.Stops {
			if stop.Key == stopKey && (stop.Type == "pickup" || (a.PlanID == "" && stop.Type == "delivery")) {
				found = true
				break
			}
		}
		if !found || len([]rune(a.ManualStopTime)) > 80 || strings.ContainsAny(a.ManualStopTime, "\r\n\t") {
			return result, ErrBoardLoadSelection
		}
		if s.ManualStopTimes == nil {
			s.ManualStopTimes = map[string]string{}
		}
		key := boardCompletionKey(*target) + "/" + stopKey
		if value := strings.TrimSpace(a.ManualStopTime); value != "" {
			s.ManualStopTimes[key] = value
		} else {
			delete(s.ManualStopTimes, key)
		}
	case "advance":
		if strings.TrimSpace(e.CurrentLoad) == "" {
			return result, ErrBoardLoadSelection
		}
		if view.Current == nil {
			plans := append(append(append([]BoardLoad{}, view.Week...), view.Next...), view.Earlier...)
			// Deduplicate plan IDs before matching an older unlinked text value.
			seen := map[string]bool{}
			unique := []BoardLoad{}
			for _, p := range plans {
				if !seen[p.PlanID] {
					unique = append(unique, p)
					seen[p.PlanID] = true
				}
			}
			view.Current = matchBoardCurrentLoad(unique, e.CurrentLoad)
			if view.Current != nil {
				next := []BoardLoad{}
				for _, p := range view.Next {
					if boardCompletionKey(p) != boardCompletionKey(*view.Current) {
						next = append(next, p)
					}
				}
				view.Next = next
			}
		}
		if a.StopKey != "" {
			if e.Status != "DISPATCHED" && e.Status != "ENROUTE" && e.Status != "RESERVED" {
				return result, ErrBoardLoadSelection
			}
			status, err := applyBoardProgressStop(&s, view, a.StopKey, e.Status == "DISPATCHED")
			if err != nil {
				return result, err
			}
			e.Status, e.Destination = status, ""
			break
		}
		switch e.Status {
		case "DISPATCHED":
			if !advanceBoardStop(&s, view, "pickup", true) {
				advanceBoardStop(&s, view, "delivery", false)
				e.Status = boardProgressStatus("ENROUTE", view.Next)
			}
			e.Destination = ""
		case "ENROUTE", "RESERVED":
			if advanceBoardStop(&s, view, "delivery", true) {
				e.Status = boardProgressStatus("ENROUTE", view.Next)
				e.Destination = ""
				break
			}
			if view.Current != nil {
				markBoardLoadCompleted(&s, *view.Current)
				delete(s.RestoredPlans, view.Current.PlanID)
			}
			s.Current = nil
			e.CurrentLoad, e.Destination, e.Status, e.ETA = "", "", "", ""
			if len(view.Next) > 0 {
				assigning = true
				next := view.Next[0]
				s.Current = &next
				e.CurrentLoad, e.Status = next.Number, "DISPATCHED"
			}
			useBoardStop(&s, "pickup")
		default:
			return result, ErrBoardLoadSelection
		}
	case "select":
		var chosen *BoardLoad
		for _, p := range append(append([]BoardLoad{}, view.Next...), view.Earlier...) {
			if p.PlanID == a.PlanID {
				v := p
				chosen = &v
				break
			}
		}
		if chosen == nil {
			return result, ErrBoardLoadSelection
		}
		chosen.PreviouslyActive, chosen.CanRestoreToQueue = false, false
		s.Current = chosen
		e.CurrentLoad = chosen.Number
		e.Status, e.Destination = "DISPATCHED", ""
		useBoardStop(&s, "pickup")
	case "clear":
		s.Current = nil
		s.StopKey = ""
		s.DestinationSource = false
		e.CurrentLoad = ""
		e.Destination = ""
		e.Status = ""
	case "order":
		valid := map[string]bool{}
		for _, p := range view.Next {
			valid[p.PlanID] = true
		}
		if len(a.Order) != len(view.Next) {
			return result, ErrBoardLoadSelection
		}
		for _, id := range a.Order {
			if !valid[id] {
				return result, ErrBoardLoadSelection
			}
			delete(valid, id)
		}
		s.Order = a.Order
		s.OrderLabels = []string{}
		for _, id := range a.Order {
			for _, p := range view.Next {
				if p.PlanID == id {
					s.OrderLabels = append(s.OrderLabels, p.Number)
				}
			}
		}
	case "reset_order":
		s.Order = nil
		s.OrderLabels = nil
	case "restore_previous":
		index := slices.IndexFunc(view.Earlier, func(p BoardLoad) bool { return p.PlanID == a.PlanID && p.PreviouslyActive && p.CanRestoreToQueue })
		if index < 0 {
			return result, ErrBoardLoadSelection
		}
		p := view.Earlier[index]
		if s.RequeuedLoads == nil {
			s.RequeuedLoads = map[string]int64{}
		}
		s.RequeuedLoads[boardCompletionKey(p)] = p.previousActiveEvent
		if s.RestoredPlans == nil {
			s.RestoredPlans = map[string]bool{}
		}
		s.RestoredPlans[p.PlanID] = true
		s.Hidden = slices.DeleteFunc(s.Hidden, func(id string) bool { return id == p.PlanID })
		e.Status = boardProgressStatus(e.Status, []BoardLoad{p})
	case "complete_queued":
		// Complete only the exact noncurrent queue entry the editor reviewed.
		// This is operational state, not a Gross Board or provider mutation.
		index := slices.IndexFunc(view.Next, func(p BoardLoad) bool { return p.PlanID == a.PlanID })
		if index < 0 || strings.EqualFold(strings.TrimSpace(view.Next[index].Number), strings.TrimSpace(e.CurrentLoad)) {
			return result, ErrBoardLoadSelection
		}
		p := view.Next[index]
		markBoardLoadCompleted(&s, p)
		delete(s.RestoredPlans, p.PlanID)
		next := slices.Delete(slices.Clone(view.Next), index, index+1)
		e.Status = boardProgressStatus(e.Status, next)
	case "restore_completed":
		index := slices.IndexFunc(view.Unavailable, func(p BoardLoad) bool {
			return p.PlanID == a.PlanID && p.CanRestoreToQueue
		})
		if index < 0 {
			return result, ErrBoardLoadSelection
		}
		p := view.Unavailable[index]
		// An explicit completion correction also clears the same worked departure.
		if p.previousActiveEvent != 0 {
			if s.RequeuedLoads == nil {
				s.RequeuedLoads = map[string]int64{}
			}
			s.RequeuedLoads[boardCompletionKey(p)] = p.previousActiveEvent
		}
		loadKey, planKey := boardCompletionKey(p), "plan:"+p.PlanID
		matchesCompletion := func(key string) bool {
			return key == loadKey || key == planKey || p.queueAlias != "" && key == "plan:"+p.queueAlias
		}
		s.Completed = slices.DeleteFunc(s.Completed, matchesCompletion)
		for stop := range s.CompletedStops {
			key, _, _ := strings.Cut(stop, "/")
			if matchesCompletion(key) {
				delete(s.CompletedStops, stop)
			}
		}
		s.Hidden = slices.DeleteFunc(s.Hidden, func(id string) bool { return id == p.PlanID })
		// A deliberate restore keeps a dated plan queued across the daily cutoff.
		if !p.Manual {
			if s.RestoredPlans == nil {
				s.RestoredPlans = make(map[string]bool)
			}
			s.RestoredPlans[p.PlanID] = true
		}
		e.Status = boardProgressStatus(e.Status, []BoardLoad{p})
	case "hide", "restore":
		list := append(append([]BoardLoad{}, view.Next...), view.Earlier...)
		if a.Action == "restore" {
			list = view.Hidden
		}
		valid := false
		for _, p := range list {
			if p.PlanID == a.PlanID {
				if a.Action == "hide" && !p.CanRemoveFromQueue {
					return result, ErrBoardGrossPlanRemoval
				}
				valid = true
			}
		}
		if !valid {
			return result, ErrBoardLoadSelection
		}
		if a.Action == "hide" {
			s.Hidden = append(s.Hidden, a.PlanID)
		} else {
			kept := []string{}
			for _, id := range s.Hidden {
				if id != a.PlanID {
					kept = append(kept, id)
				}
			}
			s.Hidden = kept
		}
	case "source":
		if view.Current == nil {
			return result, ErrBoardLoadSelection
		}
		found := false
		for _, stop := range view.Current.Stops {
			if stop.Key == a.StopKey && stop.Location != "" {
				found = true
			}
		}
		if !found {
			return result, ErrBoardLoadSelection
		}
		s.Current = view.Current
		s.StopKey = a.StopKey
		s.DestinationSource = true
		s.DestinationManual = false
	case "manual":
		if view.DestinationSource && view.SourceDestination != "" {
			e.Destination = view.SourceDestination
		}
		s.Current = view.Current
		s.DestinationSource = false
		s.DestinationManual = true
	default:
		return result, ErrBoardLoadSelection
	}
	if e.CurrentLoad == "" && (a.Action == "clear" || a.Action == "advance") {
		e.Status, err = boardClearedLoadStatus(ctx, tx, a.DriverID)
		if err != nil {
			return result, err
		}
	}
	if err = setBoardActor(ctx, tx, actor, "board"); err != nil {
		return result, err
	}
	if assigning {
		queueOnly := a.Action == "add" || a.Action == "restore" || a.Action == "restore_previous"
		returned, err := prepareBoardLoadAssignment(ctx, tx, a.DriverID, "", !queueOnly)
		if err != nil {
			return result, err
		}
		if returned && queueOnly {
			e.Status = "READY"
		}
	}
	if a.Action == "source" || a.Action == "select" || a.Action == "clear" || a.Action == "advance" || a.Action == "manual" {
		e.ETA = ""
		s.AutomationContext = ""
		s.ArrivalOverride = ""
		s.TravelMode = ""
		s.ConfirmedLoadID = nil
	}
	// Queue changes affect the loaded-state label, never manual exceptions.
	if a.Action == "hide" || a.Action == "restore" {
		next := append([]BoardLoad{}, view.Next...)
		if a.Action == "hide" {
			for i, p := range next {
				if p.PlanID == a.PlanID {
					next = append(next[:i], next[i+1:]...)
					break
				}
			}
		}
		if a.Action == "restore" {
			for _, p := range view.Hidden {
				if p.PlanID == a.PlanID && boardPlanQueued(p, s, view.FromDate) {
					next = append(next, p)
				}
			}
		}
		e.Status = boardProgressStatus(e.Status, next)
	}
	saved, err := saveDriverBoardEntries(ctx, tx, []DriverBoardEntry{e}, boardLoadToday(r.now()), false)
	if err != nil {
		return result, err
	}
	if err = saveLoadState(ctx, tx, a.DriverID, s); err != nil {
		return result, err
	}
	if err = syncFleetStatusProgress(ctx, tx, []string{a.DriverID}); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT status,version FROM driver_board WHERE driver_id=$1`, a.DriverID).Scan(&saved[0].Status, &saved[0].Version); err != nil {
		return result, err
	}
	views, err = readBoardLoads(ctx, tx, []string{a.DriverID}, a.FromDate, r.now())
	if err != nil {
		return result, err
	}
	result = BoardLoadResult{Entry: saved[0], Loads: views[a.DriverID]}
	result.Entry.Status = boardProgressStatus(result.Entry.Status, result.Loads.Next)
	if err = attachBoardUndoID(ctx, tx, &result.Entry); err != nil {
		return result, err
	}
	if err = attachBoardFleetStatus(ctx, tx, &result.Entry); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
