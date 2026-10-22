package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrPTIConflict = errors.New("PTI changed since you opened it. Reload and review the latest entry")

type PTICrew struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Availability string `json:"availability"`
	Employment   string `json:"employment"`
}
type PTIRow struct {
	WeekStart           string     `json:"weekStart"`
	TruckID             string     `json:"truckId"`
	TruckUnit           string     `json:"truckUnit"`
	Crew                []PTICrew  `json:"crew"`
	Condition           string     `json:"condition"`
	SnapshotEligibility string     `json:"snapshotEligibility"`
	SnapshotReason      string     `json:"snapshotReason"`
	Eligibility         string     `json:"eligibility"`
	EligibilityReason   string     `json:"eligibilityReason"`
	Received            bool       `json:"received"`
	ReceivedAt          *time.Time `json:"receivedAt"`
	ReceivedBy          string     `json:"receivedBy"`
	Review              string     `json:"review"`
	ReviewedAt          *time.Time `json:"reviewedAt"`
	ReviewedBy          string     `json:"reviewedBy"`
	VideoURL            string     `json:"videoUrl"`
	Notes               string     `json:"notes"`
	UpdatedAt           *time.Time `json:"updatedAt"`
	UpdatedBy           string     `json:"updatedBy"`
	Version             int64      `json:"version"`
}
type PTIWeek struct {
	WeekStart    string     `json:"weekStart"`
	Exists       bool       `json:"exists"`
	CreatedAt    *time.Time `json:"createdAt"`
	CreatedBy    string     `json:"createdBy"`
	LateSnapshot bool       `json:"lateSnapshot"`
	Rows         []PTIRow   `json:"rows"`
}
type PTIChange struct {
	RequestID         string `json:"requestId"`
	Version           int64  `json:"version"`
	Eligibility       string `json:"eligibility"`
	EligibilityReason string `json:"eligibilityReason"`
	Received          bool   `json:"received"`
	Review            string `json:"review"`
	VideoURL          string `json:"videoUrl"`
	Notes             string `json:"notes"`
}
type PTIEvent struct {
	ID        string    `json:"id"`
	Before    PTIRow    `json:"before"`
	After     PTIRow    `json:"after"`
	ActorName string    `json:"actorName"`
	CreatedAt time.Time `json:"createdAt"`
}

func PTICurrentWeek(now time.Time) string {
	zone, _ := time.LoadLocation("America/New_York")
	local := now.In(zone)
	return local.AddDate(0, 0, -(int(local.Weekday())+6)%7).Format("2006-01-02")
}

// Unknown Monday data is never replaced with today's status. Team eligibility is
// per truck: one available employed crew member is enough to require one video.
func ptiEligibility(active, late bool, condition string, crew []PTICrew) (string, string) {
	if late {
		return "unknown", "Monday snapshot captured late; confirm Monday eligibility"
	}
	if !active {
		return "exempt", "Inactive truck"
	}
	if condition == "shop" || condition == "out_of_service" {
		return "exempt", "Truck in " + condition
	}
	if len(crew) == 0 {
		return "exempt", "No assigned driver at Monday start"
	}
	unknown := false
	for _, d := range crew {
		if d.Employment == "active" && d.Availability == "active" {
			if condition == "" {
				return "unknown", "Truck condition at Monday start is unknown"
			}
			return "required", "Available driver and truck at Monday start"
		}
		if d.Employment == "" || (d.Employment == "active" && d.Availability == "") {
			unknown = true
		}
	}
	if unknown {
		return "unknown", "Driver status at Monday start is unknown"
	}
	return "exempt", "All assigned drivers unavailable at Monday start"
}

func (r *FleetRepository) EnsureCurrentPTIWeek(ctx context.Context) error {
	var enabled bool
	if err := r.pool.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil || !enabled {
		return err
	}
	return r.CreatePTIWeek(ctx, PTICurrentWeek(time.Now()), "")
}

func (r *FleetRepository) CreatePTIWeek(ctx context.Context, week, actor string) error {
	return r.createPTIWeek(ctx, week, actor, time.Now())
}

func (r *FleetRepository) createPTIWeek(ctx context.Context, week, actor string, now time.Time) error {
	if week != PTICurrentWeek(now) {
		return chargeInvalid("Only the current week's checklist can be created")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT fleet_status_lock()`); err != nil {
		return err
	}
	zone, _ := time.LoadLocation("America/New_York")
	boundary, err := time.ParseInLocation("2006-01-02", week, zone)
	if err != nil {
		return err
	}
	late := now.Sub(boundary) > 5*time.Minute
	var enabled, exists bool
	var author string
	err = tx.QueryRow(ctx, `SELECT fleet_status_enabled(),EXISTS(SELECT 1 FROM weekly_pti_weeks WHERE week_start=$1::date),
	 CASE WHEN $2='' THEN 'System' ELSE coalesce((SELECT username FROM app_users WHERE id::text=$2),'System') END`, week, actor).Scan(&enabled, &exists, &author)
	if err != nil {
		return err
	}
	if exists {
		return tx.Commit(ctx)
	}
	if !enabled {
		return chargeInvalid("Activate shared fleet statuses before starting PTI tracking")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weekly_pti_weeks(week_start,created_by,late_snapshot) VALUES($1,$2,$3)`, week, author, late); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `WITH boundary AS (SELECT $1::date::timestamp AT TIME ZONE 'America/New_York' AS at)
 SELECT t.id::text,t.unit_number,t.active,
 coalesce((SELECT h.status FROM fleet_status_history h,boundary b WHERE h.entity_kind='truck' AND h.entity_id=t.id AND h.track='condition' AND h.effective_at<=b.at ORDER BY h.effective_at DESC,h.id DESC LIMIT 1),''),
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',d.id,'name',d.full_name,
 'availability',coalesce((SELECT status FROM fleet_status_history h WHERE h.entity_kind='driver' AND h.entity_id=d.id AND h.track='availability' AND h.effective_at<=b.at ORDER BY h.effective_at DESC,h.id DESC LIMIT 1),''),
 'employment',coalesce((SELECT status FROM fleet_status_history h WHERE h.entity_kind='driver' AND h.entity_id=d.id AND h.track='employment' AND h.effective_at<=b.at ORDER BY h.effective_at DESC,h.id DESC LIMIT 1),'')) ORDER BY d.full_name)
 FROM drivers d CROSS JOIN boundary b WHERE d.id IN (
 SELECT a.driver_id FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.assigned_at<=b.at AND (a.unassigned_at IS NULL OR a.unassigned_at>b.at)
 UNION SELECT team.team_driver_id FROM driver_teams team JOIN truck_driver_assignments a ON a.driver_id=team.main_driver_id
 WHERE a.truck_id=t.id AND a.assigned_at<=b.at AND (a.unassigned_at IS NULL OR a.unassigned_at>b.at) AND team.start_week<=$1::date AND (team.end_week IS NULL OR team.end_week>$1::date)
 )), '[]'::jsonb)
 FROM trucks t CROSS JOIN boundary b WHERE t.created_at<=b.at ORDER BY t.unit_number`, week)
	if err != nil {
		return err
	}
	entries := []PTIRow{}
	for rows.Next() {
		var row PTIRow
		var active bool
		var crew []byte
		if err = rows.Scan(&row.TruckID, &row.TruckUnit, &active, &row.Condition, &crew); err != nil {
			break
		}
		if err = json.Unmarshal(crew, &row.Crew); err != nil {
			break
		}
		row.WeekStart = week
		row.Version = 1
		row.Review = "pending"
		row.Eligibility, row.EligibilityReason = ptiEligibility(active, late, row.Condition, row.Crew)
		row.SnapshotEligibility = row.Eligibility
		row.SnapshotReason = row.EligibilityReason
		entries = append(entries, row)
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	for _, row := range entries {
		raw, e := json.Marshal(row)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO weekly_pti_rows(week_start,truck_id,data) VALUES($1,$2,$3)`, week, row.TruckID, raw); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *FleetRepository) PTIWeek(ctx context.Context, week string) (PTIWeek, error) {
	result := PTIWeek{WeekStart: week, Rows: []PTIRow{}}
	err := r.pool.QueryRow(ctx, `SELECT created_at,created_by,late_snapshot FROM weekly_pti_weeks WHERE week_start=$1`, week).Scan(&result.CreatedAt, &result.CreatedBy, &result.LateSnapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Exists = true
	rows, err := r.pool.Query(ctx, `SELECT version,data FROM weekly_pti_rows WHERE week_start=$1 ORDER BY data->>'truckUnit'`, week)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var row PTIRow
		var version int64
		var raw []byte
		if err = rows.Scan(&version, &raw); err != nil {
			return result, err
		}
		if err = json.Unmarshal(raw, &row); err != nil {
			return result, err
		}
		row.Version = version
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

func (in *PTIChange) Validate() error {
	in.Notes = strings.TrimSpace(in.Notes)
	in.VideoURL = strings.TrimSpace(in.VideoURL)
	in.EligibilityReason = strings.TrimSpace(in.EligibilityReason)
	if in.Version < 1 || len(in.Notes) > 5000 || len(in.EligibilityReason) > 1000 || len(in.VideoURL) > 2000 || strings.ContainsRune(in.Notes+in.VideoURL+in.EligibilityReason, '\x00') {
		return chargeInvalid("Invalid PTI version or text length")
	}
	if in.Eligibility != "required" && in.Eligibility != "exempt" && in.Eligibility != "unknown" {
		return chargeInvalid("Choose required, exempt or needs confirmation")
	}
	if in.Review != "pending" && in.Review != "checked" && in.Review != "issues" {
		return chargeInvalid("Invalid PTI review status")
	}
	if !in.Received && (in.Review != "pending" || in.VideoURL != "") {
		return chargeInvalid("A video must be received before linking or reviewing it")
	}
	if in.Review == "issues" && in.Notes == "" {
		return chargeInvalid("Describe the issues in Notes")
	}
	if in.VideoURL != "" {
		u, err := url.Parse(in.VideoURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return chargeInvalid("Use an http or https video link without embedded credentials")
		}
	}
	return nil
}

func (r *FleetRepository) ChangePTI(ctx context.Context, week, truck, actor string, in PTIChange) (PTIRow, error) {
	var row PTIRow
	if err := in.Validate(); err != nil {
		return row, err
	}
	body, _ := json.Marshal([]any{week, truck, actor, in})
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return row, err
	}
	defer tx.Rollback(ctx)
	// Lock only this checklist row; fleet, dispatch and financial records are read-only.
	var raw []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT version,data FROM weekly_pti_rows WHERE week_start=$1 AND truck_id=$2 FOR UPDATE`, week, truck).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
	}
	if err = json.Unmarshal(raw, &row); err != nil {
		return row, err
	}
	row.Version = version
	var priorHash string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM weekly_pti_events WHERE id=$1`, in.RequestID).Scan(&priorHash)
	if err == nil {
		if priorHash != hash {
			return row, ErrPTIConflict
		}
		return row, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return row, err
	}
	if row.Version != in.Version {
		return row, ErrPTIConflict
	}
	if (row.Eligibility != in.Eligibility || row.EligibilityReason != in.EligibilityReason) && in.EligibilityReason == "" {
		return row, chargeInvalid("Explain the Monday eligibility correction")
	}
	if row.Received == in.Received && row.Review == in.Review && row.VideoURL == in.VideoURL && row.Notes == in.Notes && row.Eligibility == in.Eligibility && row.EligibilityReason == in.EligibilityReason {
		return row, tx.Commit(ctx)
	}
	before := row
	var author string
	if err = tx.QueryRow(ctx, `SELECT username FROM app_users WHERE id=$1 AND active`, actor).Scan(&author); err != nil {
		return row, err
	}
	now := time.Now().UTC()
	if row.Received != in.Received {
		row.ReceivedAt = nil
		row.ReceivedBy = ""
		if in.Received {
			row.ReceivedAt = &now
			row.ReceivedBy = author
		}
	}
	review := in.Review
	if row.VideoURL != in.VideoURL && row.Review != "pending" {
		review = "pending"
	}
	if row.Review != review {
		row.ReviewedAt = nil
		row.ReviewedBy = ""
		if review != "pending" {
			row.ReviewedAt = &now
			row.ReviewedBy = author
		}
	}
	row.Received = in.Received
	row.Review = review
	row.VideoURL = in.VideoURL
	row.Notes = in.Notes
	row.Eligibility = in.Eligibility
	row.EligibilityReason = in.EligibilityReason
	row.UpdatedAt = &now
	row.UpdatedBy = author
	row.Version++
	after, _ := json.Marshal(row)
	beforeRaw, _ := json.Marshal(before)
	if _, err = tx.Exec(ctx, `UPDATE weekly_pti_rows SET version=$3,data=$4 WHERE week_start=$1 AND truck_id=$2`, week, truck, row.Version, after); err != nil {
		return row, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weekly_pti_events(id,request_hash,week_start,truck_id,before_data,after_data,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, in.RequestID, hash, week, truck, beforeRaw, after, actor, author); err != nil {
		return row, err
	}
	return row, tx.Commit(ctx)
}

func (r *FleetRepository) PTIHistory(ctx context.Context, week, truck string) ([]PTIEvent, error) {
	events := []PTIEvent{}
	rows, err := r.pool.Query(ctx, `SELECT id::text,before_data,after_data,actor_name,created_at FROM weekly_pti_events WHERE week_start=$1 AND truck_id=$2 ORDER BY created_at DESC,id DESC LIMIT 200`, week, truck)
	if err != nil {
		return events, err
	}
	defer rows.Close()
	for rows.Next() {
		var e PTIEvent
		var before, after []byte
		if err = rows.Scan(&e.ID, &before, &after, &e.ActorName, &e.CreatedAt); err != nil {
			return events, err
		}
		if err = json.Unmarshal(before, &e.Before); err != nil {
			return events, err
		}
		if err = json.Unmarshal(after, &e.After); err != nil {
			return events, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
