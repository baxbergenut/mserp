package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/datatruck"
)

type BoardSchedule struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
	Reason  string `json:"reason,omitempty"`
	BeginAt string `json:"beginAt,omitempty"`
	EndAt   string `json:"endAt,omitempty"`
}

// Only explicit provider schedule classifications are eligible. Legacy timestamps
// are retained for details, but are not evidence of a fixed or all-day appointment.
func boardSchedule(s datatruck.LoadStop) *BoardSchedule {
	out := &BoardSchedule{Kind: s.ScheduleType, BeginAt: s.ScheduledBeginAt, EndAt: s.ScheduledEndAt}
	date, err := time.Parse(time.DateOnly, s.ScheduledBeginDate)
	clock, clockErr := time.Parse("15:04:05", s.ScheduledBeginTime)
	if err != nil || clockErr != nil {
		out.Reason = "Appointment schedule is missing a valid local date/time"
		return out
	}
	prefix := "PU"
	if s.StopType == "delivery" {
		prefix = "DEL"
	} else if s.StopType != "pickup" {
		out.Reason = "Unsupported stop type"
		return out
	}
	switch s.ScheduleType {
	case "appointment":
		if s.ScheduledEndDate != "" || s.ScheduledEndTime != "" {
			out.Reason = "Exact appointment has conflicting end fields"
			return out
		}
		out.Label = fmt.Sprintf("%s %s %s", prefix, clock.Format("15:04"), strings.ToUpper(date.Format("Mon")))
	case "window":
		endDate, e := time.Parse(time.DateOnly, s.ScheduledEndDate)
		endClock, e2 := time.Parse("15:04:05", s.ScheduledEndTime)
		if e != nil || e2 != nil || endDate.Format(time.DateOnly)+endClock.Format("15:04:05") < date.Format(time.DateOnly)+clock.Format("15:04:05") {
			out.Reason = "Appointment window is incomplete or reversed"
			return out
		}
		out.Label = fmt.Sprintf("%s 24/7 %s", prefix, strings.ToUpper(date.Format("Mon")))
	default:
		// Activate other provider enums only after their semantics are verified.
		out.Reason = "Appointment type requires provider verification"
		return out
	}
	out.Detail = fmt.Sprintf("%s %s (facility local time)", s.ScheduledBeginDate, s.ScheduledBeginTime)
	if s.ScheduleType == "window" {
		out.Detail = fmt.Sprintf("%s %s – %s %s (facility local time)", s.ScheduledBeginDate, s.ScheduledBeginTime, s.ScheduledEndDate, s.ScheduledEndTime)
	}
	return out
}

func boardStopCoordinates(s datatruck.LoadStop) (*float64, *float64) {
	if s.Location.Latitude == nil || s.Location.Longitude == nil {
		return nil, nil
	}
	lat, e := strconv.ParseFloat(string(*s.Location.Latitude), 64)
	lng, e2 := strconv.ParseFloat(string(*s.Location.Longitude), 64)
	if e != nil || e2 != nil || math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) || lat < -90 || lat > 90 || lng < -180 || lng > 180 || (lat == 0 && lng == 0) {
		return nil, nil
	}
	return &lat, &lng
}

type BoardAutomation struct {
	Context              string          `json:"context"`
	Appointment          *BoardSchedule  `json:"appointment,omitempty"`
	Arrived              bool            `json:"arrived"`
	ArrivalOverride      string          `json:"arrivalOverride"`
	ObservedAt           time.Time       `json:"observedAt"`
	Stale                bool            `json:"stale"`
	TravelMode           string          `json:"travelMode"`
	TravelValue          string          `json:"travelValue"`
	Reason               string          `json:"reason,omitempty"`
	RequiresConfirmation bool            `json:"requiresConfirmation"`
	RoutingEnabled       bool            `json:"routingEnabled"`
	ManualETAValid       bool            `json:"manualETAValid"`
	TelemetrySafe        bool            `json:"telemetrySafe"`
	Evidence             ArrivalEvidence `json:"-"`
}

type ArrivalEvidence struct {
	Context      string    `json:"context"`
	Arrived      bool      `json:"arrived"`
	LastAt       time.Time `json:"lastAt"`
	InsideSince  time.Time `json:"insideSince"`
	OutsideCount int       `json:"outsideCount"`
}

func distanceMeters(a, b, c, d float64) float64 {
	const rad = math.Pi / 180
	x := math.Sin((c - a) * rad / 2)
	y := math.Sin((d - b) * rad / 2)
	h := x*x + math.Cos(a*rad)*math.Cos(c*rad)*y*y
	return 6371000 * 2 * math.Asin(math.Sqrt(math.Min(1, h)))
}

func evaluateArrival(old ArrivalEvidence, key string, location *DriverBoardLocation, stop BoardStop, now time.Time) ArrivalEvidence {
	if old.Context != key {
		old = ArrivalEvidence{Context: key}
	}
	if location == nil || stop.Latitude == nil || stop.Longitude == nil || !location.ReportedAt.After(old.LastAt) || location.ReportedAt.After(now) || now.Sub(location.ReportedAt) > 10*time.Minute {
		return old
	}
	// A gap breaks dwell/departure continuity, but preserves last-known arrival.
	if !old.LastAt.IsZero() && location.ReportedAt.Sub(old.LastAt) > 10*time.Minute {
		old.InsideSince = time.Time{}
		old.OutsideCount = 0
	}
	old.LastAt = location.ReportedAt
	distance := distanceMeters(location.Latitude, location.Longitude, *stop.Latitude, *stop.Longitude)
	switch {
	case distance <= 150:
		old.OutsideCount = 0
		if old.InsideSince.IsZero() {
			old.InsideSince = location.ReportedAt
		} else if location.ReportedAt.Sub(old.InsideSince) >= 3*time.Minute {
			old.Arrived = true
		}
	case distance > 300:
		old.InsideSince = time.Time{}
		old.OutsideCount++
		if old.OutsideCount >= 2 {
			old.Arrived = false
		}
	default:
		old.InsideSince = time.Time{}
		old.OutsideCount = 0
	}
	return old
}

func boardAutomationContext(load *BoardLoad, stop *BoardStop, truck string) string {
	if load == nil || load.LoadID == nil || stop == nil {
		return ""
	}
	body, _ := json.Marshal([]any{*load.LoadID, stop.Key, truck, stop.Latitude, stop.Longitude})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Resolve a unique imported load without manufacturing a Gross Board plan.
func importedBoardLoad(ctx context.Context, tx pgx.Tx, number string) (*BoardLoad, error) {
	rows, err := tx.Query(ctx, `SELECT id,load_id,status,coalesce(driver_id::text,''),synced_at::text,raw_payload FROM loads WHERE lower(btrim(load_id))=lower(btrim($1)) ORDER BY id LIMIT 2`, number)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chosen *BoardLoad
	for rows.Next() {
		var p BoardLoad
		var id int
		var raw []byte
		if err = rows.Scan(&id, &p.Number, &p.SourceStatus, &p.SourceDriver, &p.SyncedAt, &raw); err != nil {
			return nil, err
		}
		if chosen != nil {
			return nil, nil
		}
		p.LoadID = &id
		p.Stops = boardStops(raw)
		chosen = &p
	}
	return chosen, rows.Err()
}

// Read cached evidence only; displaying the board never advances the detector.
func enrichBoardAutomation(ctx context.Context, tx pgx.Tx, views map[string]BoardLoads, states map[string]boardLoadState) error {
	ids := make([]string, 0, len(views))
	for id := range views {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT d.id,coalesce(t.id::text,'')||'/'||coalesce(a.id::text,'')||'/'||coalesce(t.vin,'')||'/'||coalesce(b.trailer_number,''),coalesce(c.evidence,'{}'::jsonb),coalesce(c.travel,'{}'::jsonb),coalesce(c.eta_context,''),coalesce(b.eta,''),
 (t.vin IS NOT NULL AND btrim(t.vin)<>'' AND (SELECT count(*) FROM trucks other WHERE upper(regexp_replace(other.vin,'[^A-Za-z0-9]','','g'))=upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g')))=1)
 FROM drivers d LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id LEFT JOIN driver_board_automation c ON c.driver_id=d.id LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.id=ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, truck, etaContext, eta string
		var evidence ArrivalEvidence
		var travel BoardTravel
		var telemetrySafe bool
		if err = rows.Scan(&id, &truck, &evidence, &travel, &etaContext, &eta, &telemetrySafe); err != nil {
			return err
		}
		v := views[id]
		s := states[id]
		var stop *BoardStop
		if v.Current != nil && v.DestinationSource {
			for i := range v.Current.Stops {
				if v.Current.Stops[i].Key == v.StopKey {
					stop = &v.Current.Stops[i]
					break
				}
			}
		}
		key := boardAutomationContext(v.Current, stop, truck)
		selectionInvalid := v.Current != nil && (strings.Contains(v.Current.Warning, "removed, replaced or reassigned") || strings.Contains(v.Current.Warning, "load match changed") || strings.Contains(v.Current.Warning, "text differs"))
		if selectionInvalid {
			key = ""
		}
		a := &BoardAutomation{Context: key, ArrivalOverride: "auto", TravelMode: "manual", Evidence: evidence, RoutingEnabled: travel.Enabled, ManualETAValid: etaContext == "" || etaContext == key}
		a.TelemetrySafe = telemetrySafe
		if selectionInvalid {
			a.Reason = "Review the changed current load selection before automation resumes"
		}
		if travel.Enabled && (eta == "" || !a.ManualETAValid) {
			a.TravelMode = "auto"
		}
		// The Status Board selection is the dispatcher's operational assignment.
		// DataTruck's driver may lag behind it; no second confirmation is needed.
		// Keep requiresConfirmation false for older clients during deployment.
		if key != "" {
			a.Appointment = stop.Schedule
			if evidence.Context == key && telemetrySafe {
				a.Arrived = evidence.Arrived
				a.ObservedAt = evidence.LastAt
				a.Stale = evidence.LastAt.IsZero() || time.Since(evidence.LastAt) > 10*time.Minute
			}
			if s.AutomationContext == key {
				if s.ArrivalOverride != "" {
					a.ArrivalOverride = s.ArrivalOverride
				}
				if s.TravelMode != "" {
					a.TravelMode = s.TravelMode
				}
			}
			if a.ArrivalOverride == "arrived" {
				a.Arrived = true
			}
			if a.ArrivalOverride == "not_arrived" {
				a.Arrived = false
			}
			if a.TravelMode == "auto" && telemetrySafe {
				a.TravelValue, a.Reason = travel.display(key, time.Now())
			}
		} else if a.Reason == "" {
			a.Reason = "Select a matched load stop and assigned truck"
		}
		if !telemetrySafe && a.Reason == "" {
			a.Reason = "GPS automation requires a unique assigned truck VIN"
		}
		v.Automation = a
		views[id] = v
	}
	return rows.Err()
}
