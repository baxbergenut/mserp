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
		begin, _ := time.Parse("2006-01-02 15:04:05", s.ScheduledBeginDate+" "+s.ScheduledBeginTime)
		end, _ := time.Parse("2006-01-02 15:04:05", s.ScheduledEndDate+" "+s.ScheduledEndTime)
		if end.Sub(begin) >= 24*time.Hour || (date.Equal(endDate) && clock.Format("15:04") == "00:00" && endClock.Format("15:04") == "23:59") {
			out.Label = fmt.Sprintf("%s 24/7 %s", prefix, strings.ToUpper(date.Format("Mon")))
		} else {
			until := ""
			if endClock.Hour() >= 22 {
				until = "TILL "
			}
			out.Label = fmt.Sprintf("%s %s%s %s", prefix, until, endClock.Format("15:04"), strings.ToUpper(endDate.Format("Mon")))
		}
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

// Project overrides onto copies of stops so source snapshots and revision fences
// retain the original provider schedule. The same identity follows a queued load
// when it becomes current.
func boardStopTimes(load BoardLoad, times map[string]string) BoardLoad {
	load.Stops = append([]BoardStop{}, load.Stops...)
	for i := range load.Stops {
		stop := &load.Stops[i]
		stop.ManualStopTime = times[boardCompletionKey(load)+"/"+stop.Key]
		if stop.ManualStopTime != "" {
			prefix := "PU"
			if stop.Type == "delivery" {
				prefix = "DEL"
			}
			stop.Schedule = &BoardSchedule{Kind: "manual", Label: prefix + " " + stop.ManualStopTime, Detail: "Manual stop time entered on Status Board"}
		}
	}
	return load
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
	ManualStopTime       string          `json:"manualStopTime"`
	StopType             string          `json:"stopType,omitempty"`
	Arrived              bool            `json:"arrived"`
	ArrivalOverride      string          `json:"arrivalOverride"`
	ObservedAt           time.Time       `json:"observedAt"`
	Stale                bool            `json:"stale"`
	TravelMode           string          `json:"travelMode"`
	TravelValue          string          `json:"travelValue"`
	TravelStale          bool            `json:"travelStale"`
	TravelArrivalAt      string          `json:"travelArrivalAt,omitempty"`
	Reason               string          `json:"reason,omitempty"`
	RequiresConfirmation bool            `json:"requiresConfirmation"`
	RoutingEnabled       bool            `json:"routingEnabled"`
	ManualETAValid       bool            `json:"manualETAValid"`
	TelemetrySafe        bool            `json:"telemetrySafe"`
	Evidence             ArrivalEvidence `json:"-"`
}

type ArrivalEvidence struct {
	Source  string    `json:"source,omitempty"`
	Context string    `json:"context"`
	Arrived bool      `json:"arrived"`
	LastAt  time.Time `json:"lastAt"`
}

const boardArrivalRadiusMeters = 5 * 1609.344

func distanceMeters(a, b, c, d float64) float64 {
	const rad = math.Pi / 180
	x := math.Sin((c - a) * rad / 2)
	y := math.Sin((d - b) * rad / 2)
	h := x*x + math.Cos(a*rad)*math.Cos(c*rad)*y*y
	return 6371000 * 2 * math.Asin(math.Sqrt(math.Min(1, h)))
}

func evaluateArrival(old ArrivalEvidence, key string, location *DriverBoardLocation, stop BoardStop, now time.Time) ArrivalEvidence {
	if old.Context != key || (location != nil && !sameTelemetrySource(old.Source, location.Source)) {
		old = ArrivalEvidence{Context: key}
	}
	if location == nil || stop.Latitude == nil || stop.Longitude == nil || location.ReportedAt.IsZero() || location.ReportedAt.Before(old.LastAt) || location.ReportedAt.After(now) {
		return old
	}
	old.LastAt = location.ReportedAt
	old.Source = location.Source
	// Re-evaluate the latest cached point even when its timestamp is unchanged:
	// an older proximity rule may have recorded a different result for that point.
	// Staleness is exposed to viewers and never treated as a live observation.
	old.Arrived = distanceMeters(location.Latitude, location.Longitude, *stop.Latitude, *stop.Longitude) <= boardArrivalRadiusMeters
	return old
}

func sameTelemetrySource(a, b string) bool {
	if a == "" {
		a = "five_eld"
	}
	if b == "" {
		b = "five_eld"
	}
	return a == b
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
	rows, err := tx.Query(ctx, `SELECT id,load_id,status,coalesce(driver_id::text,''),synced_at::text,raw_payload FROM clustie_loads WHERE lower(btrim(load_id))=lower(btrim($1)) ORDER BY id LIMIT 2`, number)
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
 (t.vin IS NOT NULL AND btrim(t.vin)<>'' AND (SELECT count(*) FROM trucks other WHERE upper(regexp_replace(other.vin,'[^A-Za-z0-9]','','g'))=upper(regexp_replace(t.vin,'[^A-Za-z0-9]','','g')))=1),coalesce(el.source,''),el.reported_at
 FROM drivers d LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id LEFT JOIN driver_board_automation c ON c.driver_id=d.id LEFT JOIN driver_board b ON b.driver_id=d.id `+statusBoardELDLocationJoinSQL+` WHERE d.id=ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, truck, etaContext, eta, telemetrySource string
		var evidence ArrivalEvidence
		var travel BoardTravel
		var telemetrySafe bool
		var gpsAt *time.Time
		if err = rows.Scan(&id, &truck, &evidence, &travel, &etaContext, &eta, &telemetrySafe, &telemetrySource, &gpsAt); err != nil {
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
			a.StopType = stop.Type
			if v.Current != nil {
				a.ManualStopTime = s.ManualStopTimes[boardCompletionKey(*v.Current)+"/"+stop.Key]
				if a.ManualStopTime != "" {
					prefix := "PU"
					if stop.Type == "delivery" {
						prefix = "DEL"
					}
					a.Appointment = &BoardSchedule{Kind: "manual", Label: prefix + " " + a.ManualStopTime, Detail: "Manual stop time entered on Status Board"}
				}
			}
			if evidence.Context == key && telemetrySafe && sameTelemetrySource(evidence.Source, telemetrySource) {
				a.Arrived = evidence.Arrived
				a.ObservedAt = evidence.LastAt
				a.Stale = evidence.LastAt.IsZero() || time.Since(evidence.LastAt) > 10*time.Minute
			}
			if a.TravelMode == "auto" && telemetrySafe && sameTelemetrySource(travel.Source, telemetrySource) {
				now := time.Now()
				var reportedAt time.Time
				if gpsAt != nil {
					reportedAt = *gpsAt
				}
				a.TravelValue, a.Reason = travel.display(key, now, reportedAt)
				a.TravelStale = travel.stale(now, reportedAt)
				if a.TravelValue != "" && !a.TravelStale && travel.Motion == "moving" && !travel.ArrivalAt.IsZero() {
					a.TravelArrivalAt = travel.ArrivalAt.Round(15 * time.Minute).Format(time.RFC3339)
				}
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
