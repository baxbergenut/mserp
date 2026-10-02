package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/datatruck"
)

var ErrBoardLoadSelection = errors.New("load plans changed or the selection is unavailable; refresh Loads and review the current plans")

type BoardStop struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Location    string `json:"location"`
	Appointment string `json:"appointment"`
}
type BoardLoad struct {
	PlanID       string      `json:"planId"`
	Date         string      `json:"date"`
	Slot         int         `json:"slot"`
	Number       string      `json:"number"`
	LoadID       *int        `json:"loadId"`
	SourceStatus string      `json:"sourceStatus"`
	SourceDriver string      `json:"sourceDriver"`
	SyncedAt     string      `json:"syncedAt"`
	Stops        []BoardStop `json:"stops"`
	Warning      string      `json:"warning"`
}
type boardLoadState struct {
	Current           *BoardLoad `json:"current"`
	Order             []string   `json:"order"`
	OrderLabels       []string   `json:"orderLabels"`
	Hidden            []string   `json:"hidden"`
	DestinationSource bool       `json:"destinationSource"`
	StopKey           string     `json:"stopKey"`
}
type BoardLoads struct {
	Current           *BoardLoad  `json:"current"`
	Next              []BoardLoad `json:"next"`
	Hidden            []BoardLoad `json:"hidden"`
	Unavailable       []BoardLoad `json:"unavailable"`
	DestinationSource bool        `json:"destinationSource"`
	SourceDestination string      `json:"sourceDestination"`
	StopKey           string      `json:"stopKey"`
	FromDate          string      `json:"fromDate"`
	Revision          string      `json:"revision"`
	CustomOrder       bool        `json:"customOrder"`
}
type BoardLoadAction struct {
	DriverID    string   `json:"driverId"`
	Version     int      `json:"version"`
	HomeVersion int      `json:"homeVersion"`
	FromDate    string   `json:"fromDate"`
	Revision    string   `json:"revision"`
	Action      string   `json:"action"`
	PlanID      string   `json:"planId"`
	Order       []string `json:"order"`
	StopKey     string   `json:"stopKey"`
}
type BoardLoadResult struct {
	Entry DriverBoardEntry `json:"entry"`
	Loads BoardLoads       `json:"loads"`
}

const boardPlansSQL = `(SELECT driver_id,service_date,0 AS slot,false AS deleted,load_number,load_record_id,day_status,plan_id FROM gross_board_entries
 UNION ALL SELECT driver_id,service_date,slot,deleted,load_number,load_record_id,day_status,plan_id FROM gross_board_extra_entries)`

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
		if location == "" {
			location = strings.TrimSpace(s.Location.Address)
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s/%s", s.Ordering, s.StopType, location, s.Location.Address)))
		stop := BoardStop{Key: hex.EncodeToString(hash[:12]), Type: s.StopType, Location: location}
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
func terminalLoad(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "delivered", "completed", "cancelled", "canceled":
		return true
	}
	return false
}
func annotateLoad(p *BoardLoad, driver string) {
	if p.LoadID == nil {
		p.Warning = "Unmatched plan"
	}
	if p.SourceDriver != "" && p.SourceDriver != driver {
		p.Warning = "DataTruck assigns this load to another driver; review the plan"
	}
	if terminalLoad(p.SourceStatus) {
		p.Warning = "DataTruck reports " + p.SourceStatus + "; review before replacing the current load"
	}
}

// Read-only reconciliation: no calendar or upstream status can promote a load.
func readBoardLoads(ctx context.Context, tx pgx.Tx, ids []string, from string) (map[string]BoardLoads, error) {
	result := map[string]BoardLoads{}
	states := map[string]boardLoadState{}
	manualMismatch := map[string]bool{}
	selected := []string{}
	for _, id := range ids {
		states[id] = boardLoadState{}
		result[id] = BoardLoads{Next: []BoardLoad{}, Hidden: []BoardLoad{}, Unavailable: []BoardLoad{}, FromDate: from}
	}
	rows, err := tx.Query(ctx, `SELECT s.driver_id,s.payload,coalesce(b.current_load,'') FROM driver_board_load_state s LEFT JOIN driver_board b ON b.driver_id=s.driver_id WHERE s.driver_id=ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var s boardLoadState
		var currentText string
		if err = rows.Scan(&id, &s, &currentText); err != nil {
			rows.Close()
			return nil, err
		}
		states[id] = s
		if s.Current != nil {
			selected = append(selected, s.Current.PlanID)
			manualMismatch[id] = currentText != s.Current.Number
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	plans := map[string][]BoardLoad{}
	rows, err = tx.Query(ctx, `SELECT e.driver_id,e.plan_id,e.service_date::text,e.slot,e.load_number,l.id,coalesce(l.status,''),coalesce(l.driver_id::text,''),coalesce(l.synced_at::text,''),coalesce(l.raw_payload,'{}'::jsonb)
 FROM `+boardPlansSQL+` e `+grossBoardResolvedLoad+`
 WHERE e.driver_id=ANY($1::uuid[]) AND NOT e.deleted AND e.day_status='' AND btrim(e.load_number)<>''
 AND (e.service_date >= $2::date OR e.plan_id=ANY($3::uuid[])
 OR coalesce((SELECT payload->'order' ? e.plan_id::text FROM driver_board_load_state WHERE driver_id=e.driver_id),false))
 ORDER BY e.driver_id,e.service_date,e.slot`, ids, from, selected)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var p BoardLoad
		var raw []byte
		if err = rows.Scan(&id, &p.PlanID, &p.Date, &p.Slot, &p.Number, &p.LoadID, &p.SourceStatus, &p.SourceDriver, &p.SyncedAt, &raw); err != nil {
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
	for _, id := range ids {
		s := states[id]
		view := result[id]
		view.DestinationSource = s.DestinationSource
		view.StopKey = s.StopKey
		view.CustomOrder = len(s.Order) > 0
		if s.Current != nil {
			current := *s.Current
			current.Warning = "Current plan was removed, replaced or reassigned in Gross Board; review it"
			for _, p := range plans[id] {
				if p.PlanID == current.PlanID {
					if current.LoadID != nil && (p.LoadID == nil || *current.LoadID != *p.LoadID) {
						current.Warning = "Current plan's load match changed; review it"
					} else {
						current = p
					}
					break
				}
			}
			view.Current = &current
			if manualMismatch[id] {
				view.Current.Warning = "Current load text differs from the selected plan; clear or reselect the load"
				view.DestinationSource = false
			}
			for _, stop := range current.Stops {
				if stop.Key == s.StopKey {
					view.SourceDestination = stop.Location
				}
			}
			if s.DestinationSource && view.SourceDestination == "" {
				view.Current.Warning = strings.TrimSpace(view.Current.Warning + " Selected stop is missing or has no location; select a stop or use a manual destination")
			}
		}
		seen := map[int]bool{}
		if view.Current != nil && view.Current.LoadID != nil {
			seen[*view.Current.LoadID] = true
		}
		hidden := map[string]bool{}
		for _, k := range s.Hidden {
			hidden[k] = true
		}
		for _, p := range plans[id] {
			if view.Current != nil && p.PlanID == view.Current.PlanID {
				continue
			}
			if p.LoadID != nil {
				if seen[*p.LoadID] {
					continue
				}
				seen[*p.LoadID] = true
			}
			if terminalLoad(p.SourceStatus) {
				view.Unavailable = append(view.Unavailable, p)
			} else if hidden[p.PlanID] {
				view.Hidden = append(view.Hidden, p)
			} else {
				view.Next = append(view.Next, p)
			}
		}
		rank := map[string]int{}
		for i, key := range s.Order {
			rank[key] = i + 1
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
		raw, _ := json.Marshal(view)
		hash := sha256.Sum256(raw)
		view.Revision = hex.EncodeToString(hash[:])
		result[id] = view
	}
	return result, nil
}

func (r *DriverBoardRepository) Loads(ctx context.Context, id, from string) (BoardLoads, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return BoardLoads{}, err
	}
	defer tx.Rollback(ctx)
	views, err := readBoardLoads(ctx, tx, []string{id}, from)
	if err != nil {
		return BoardLoads{}, err
	}
	return views[id], tx.Commit(ctx)
}
func boardEntry(ctx context.Context, tx pgx.Tx, id string) (DriverBoardEntry, error) {
	var e DriverBoardEntry
	err := tx.QueryRow(ctx, `SELECT d.id,coalesce(b.current_load,''),coalesce(b.trailer_number,''),coalesce(b.status,''),coalesce(b.destination,''),coalesce(b.eta,''),coalesce(b.notes,''),coalesce(b.home_time,''),d.driver_home,d.driver_home_version,coalesce(b.version,0)
 FROM drivers d LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.id=$1`, id).Scan(&e.DriverID, &e.CurrentLoad, &e.TrailerNumber, &e.Status, &e.Destination, &e.ETA, &e.Notes, &e.HomeTime, &e.DriverHome, &e.HomeVersion, &e.Version)
	return e, err
}
func (r *DriverBoardRepository) ChangeLoads(ctx context.Context, a BoardLoadAction, actor string) (BoardLoadResult, error) {
	var result BoardLoadResult
	tx, err := r.pool.Begin(ctx)
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
	if !active || e.Version != a.Version || e.HomeVersion != a.HomeVersion {
		return result, ErrDriverBoardConflict
	}
	views, err := readBoardLoads(ctx, tx, []string{a.DriverID}, a.FromDate)
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
	switch a.Action {
	case "select":
		var chosen *BoardLoad
		for _, p := range view.Next {
			if p.PlanID == a.PlanID {
				v := p
				chosen = &v
				break
			}
		}
		if chosen == nil {
			return result, ErrBoardLoadSelection
		}
		s.Current = chosen
		s.StopKey = a.StopKey
		s.DestinationSource = false
		if a.StopKey != "" {
			for _, stop := range chosen.Stops {
				if stop.Key == a.StopKey && stop.Location != "" {
					s.DestinationSource = true
				}
			}
			if !s.DestinationSource {
				return result, ErrBoardLoadSelection
			}
		}
		e.CurrentLoad = chosen.Number
	case "clear":
		s.Current = nil
		s.StopKey = ""
		s.DestinationSource = false
		e.CurrentLoad = ""
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
	case "hide", "restore":
		list := view.Next
		if a.Action == "restore" {
			list = view.Hidden
		}
		valid := false
		for _, p := range list {
			if p.PlanID == a.PlanID {
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
	case "manual":
		if s.DestinationSource && view.SourceDestination != "" {
			e.Destination = view.SourceDestination
		}
		s.DestinationSource = false
	default:
		return result, ErrBoardLoadSelection
	}
	if err = setBoardActor(ctx, tx, actor, "board"); err != nil {
		return result, err
	}
	saved, err := saveDriverBoardEntries(ctx, tx, []DriverBoardEntry{e})
	if err != nil {
		return result, err
	}
	if err = saveLoadState(ctx, tx, a.DriverID, s); err != nil {
		return result, err
	}
	views, err = readBoardLoads(ctx, tx, []string{a.DriverID}, a.FromDate)
	if err != nil {
		return result, err
	}
	result = BoardLoadResult{Entry: saved[0], Loads: views[a.DriverID]}
	return result, tx.Commit(ctx)
}
