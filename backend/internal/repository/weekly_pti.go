package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	ImportSource        json.RawMessage `json:"importSource,omitempty"`
	Mark                *string         `json:"mark,omitempty"`
	WeekStart           string          `json:"weekStart"`
	TruckID             string          `json:"truckId"`
	TruckUnit           string          `json:"truckUnit"`
	Crew                []PTICrew       `json:"crew"`
	Condition           string          `json:"condition"`
	SnapshotEligibility string          `json:"snapshotEligibility"`
	SnapshotReason      string          `json:"snapshotReason"`
	Eligibility         string          `json:"eligibility"`
	EligibilityReason   string          `json:"eligibilityReason"`
	Received            bool            `json:"received"`
	ReceivedAt          *time.Time      `json:"receivedAt"`
	ReceivedBy          string          `json:"receivedBy"`
	Review              string          `json:"review"`
	ReviewedAt          *time.Time      `json:"reviewedAt"`
	ReviewedBy          string          `json:"reviewedBy"`
	VideoURL            string          `json:"videoUrl"`
	Notes               string          `json:"notes"`
	UpdatedAt           *time.Time      `json:"updatedAt"`
	UpdatedBy           string          `json:"updatedBy"`
	Version             int64           `json:"version"`
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
	UndoRequestID     string  `json:"undoRequestId,omitempty"`
	Mark              *string `json:"mark,omitempty"`
	RequestID         string  `json:"requestId"`
	Version           int64   `json:"version"`
	Eligibility       string  `json:"eligibility"`
	EligibilityReason string  `json:"eligibilityReason"`
	Received          bool    `json:"received"`
	Review            string  `json:"review"`
	VideoURL          string  `json:"videoUrl"`
	Notes             string  `json:"notes"`
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

// Eligibility follows shared fleet status, never manual PTI overrides.
// One available employed team member is enough for the shared truck.
func ptiEligibility(active bool, condition string, crew []PTICrew) (string, string) {
	if !active {
		return "exempt", "Inactive truck"
	}
	if condition != "ready" && condition != "in_use" {
		return "exempt", "Truck is not ready for the road"
	}
	for _, d := range crew {
		if d.Employment == "active" && d.Availability == "active" {
			return "required", "Available driver and road-ready truck"
		}
	}
	return "exempt", "No available assigned driver"
}

func (r *FleetRepository) EnsureCurrentPTIWeek(ctx context.Context) error {
	now := time.Now()
	week := PTICurrentWeek(now)
	var enabled, exists bool
	if err := r.pool.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil || !enabled {
		return err
	}
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weekly_pti_weeks WHERE week_start=$1)`, week).Scan(&exists); err != nil {
		return err
	}
	if exists {
		changed, err := changedPTIRows(ctx, r.pool, week, now)
		if err != nil || len(changed) == 0 {
			return err
		}
	}
	return r.createPTIWeek(ctx, week, "", now)
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
	var enabled bool
	var author string
	err = tx.QueryRow(ctx, `SELECT fleet_status_enabled(),CASE WHEN $1='' THEN 'System' ELSE coalesce((SELECT username FROM app_users WHERE id::text=$1),'System') END`, actor).Scan(&enabled, &author)
	if err != nil {
		return err
	}
	if !enabled {
		return chargeInvalid("Activate shared fleet statuses before starting PTI tracking")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weekly_pti_weeks(week_start,created_by,late_snapshot) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, week, author); err != nil {
		return err
	}
	entries, err := changedPTIRows(ctx, tx, week, now)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return tx.Commit(ctx)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	// Keep marks, reviews and initial eligibility. Stable trucks do not even
	// enter the conflict path or take a row lock on each minute tick.
	_, err = tx.Exec(ctx, `WITH incoming AS (
 SELECT (data->>'truckId')::uuid AS truck_id, data FROM jsonb_array_elements($2::jsonb) AS rows(data)
 )
 INSERT INTO weekly_pti_rows(week_start,truck_id,data)
 SELECT $1,i.truck_id,i.data FROM incoming i LEFT JOIN weekly_pti_rows p ON p.week_start=$1 AND p.truck_id=i.truck_id
 WHERE p.truck_id IS NULL OR (p.data->'truckUnit',p.data->'crew',p.data->'condition',p.data->'eligibility',p.data->'eligibilityReason')
 IS DISTINCT FROM (i.data->'truckUnit',i.data->'crew',i.data->'condition',i.data->'eligibility',i.data->'eligibilityReason')
 ORDER BY i.truck_id ON CONFLICT(week_start,truck_id) DO UPDATE SET version=weekly_pti_rows.version+1,
 data=weekly_pti_rows.data || jsonb_build_object('truckUnit',EXCLUDED.data->'truckUnit','crew',EXCLUDED.data->'crew','condition',EXCLUDED.data->'condition','eligibility',EXCLUDED.data->'eligibility','eligibilityReason',EXCLUDED.data->'eligibilityReason')
 WHERE (weekly_pti_rows.data->'truckUnit',weekly_pti_rows.data->'crew',weekly_pti_rows.data->'condition',weekly_pti_rows.data->'eligibility',weekly_pti_rows.data->'eligibilityReason')
 IS DISTINCT FROM (EXCLUDED.data->'truckUnit',EXCLUDED.data->'crew',EXCLUDED.data->'condition',EXCLUDED.data->'eligibility',EXCLUDED.data->'eligibilityReason')`, week, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Compute current eligibility from the same dated roster for both the idle check
// and the locked write. Group the fleet's assignments and teams in one pass.
func changedPTIRows(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, week string, now time.Time) ([]PTIRow, error) {
	rows, err := q.Query(ctx, `WITH assigned AS MATERIALIZED (
 SELECT truck_id,driver_id FROM truck_driver_assignments WHERE assigned_at<=$2 AND (unassigned_at IS NULL OR unassigned_at>$2)
 ), members AS (
 SELECT truck_id,driver_id FROM assigned
 UNION SELECT a.truck_id,t.team_driver_id FROM assigned a JOIN driver_teams t ON t.main_driver_id=a.driver_id
 WHERE t.start_week<=($2::timestamptz AT TIME ZONE 'America/New_York')::date
 AND (t.end_week IS NULL OR t.end_week>($2::timestamptz AT TIME ZONE 'America/New_York')::date)
 ), crews AS (
 SELECT m.truck_id,jsonb_agg(jsonb_build_object('id',d.id,'name',d.full_name,'availability',d.availability_status,
 'employment',CASE WHEN d.active THEN 'active' ELSE 'terminated' END) ORDER BY d.full_name,d.id) AS crew
 FROM members m JOIN drivers d ON d.id=m.driver_id GROUP BY m.truck_id
 ), operators AS MATERIALIZED (
 SELECT DISTINCT fleet_status_driver_truck(id) AS truck FROM drivers WHERE active
 ), current_rows AS MATERIALIZED (
 SELECT t.id,t.unit_number,t.active,`+boardTruckConditionSQL+`
 WHEN o.truck IS NOT NULL THEN 'in_use' ELSE 'ready' END AS condition,coalesce(c.crew,'[]'::jsonb) AS crew,p.data
 FROM trucks t LEFT JOIN operators o ON o.truck=t.id LEFT JOIN crews c ON c.truck_id=t.id LEFT JOIN weekly_pti_rows p ON p.week_start=$1 AND p.truck_id=t.id
 WHERE t.created_at<=$2 OR p.truck_id IS NOT NULL
 )
 SELECT id::text,unit_number,active,condition,crew,
 (data->>'truckUnit',data->'crew',data->>'condition') IS NOT DISTINCT FROM (unit_number,crew,condition),
 coalesce(data->>'eligibility',''),coalesce(data->>'eligibilityReason','') FROM current_rows`, week, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var changed []PTIRow
	for rows.Next() {
		var row PTIRow
		var active, sameContext bool
		var oldEligibility, oldReason string
		var crew []byte
		if err := rows.Scan(&row.TruckID, &row.TruckUnit, &active, &row.Condition, &crew, &sameContext, &oldEligibility, &oldReason); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(crew, &row.Crew); err != nil {
			return nil, err
		}
		row.Eligibility, row.EligibilityReason = ptiEligibility(active, row.Condition, row.Crew)
		if sameContext && oldEligibility == row.Eligibility && oldReason == row.EligibilityReason {
			continue
		}
		row.WeekStart, row.Review, row.Version = week, "pending", 1
		row.SnapshotEligibility, row.SnapshotReason = row.Eligibility, row.EligibilityReason
		changed = append(changed, row)
	}
	return changed, rows.Err()
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
	if in.Mark != nil {
		if *in.Mark != "" && *in.Mark != "/" && *in.Mark != "1" && *in.Mark != "0" {
			return chargeInvalid("PTI must be /, 1 or 0")
		}
		in.Received = *in.Mark == "1"
		in.Review = "pending"
		if in.Received {
			in.Review = "checked"
		}
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Version < 0 || (in.Version == 0 && in.Mark == nil) || len(in.Notes) > 5000 || strings.ContainsRune(in.Notes, '\x00') {
		return chargeInvalid("Invalid PTI version or notes length")
	}
	if in.Review != "pending" && in.Review != "checked" && in.Review != "issues" {
		return chargeInvalid("Invalid PTI review status")
	}
	if !in.Received && in.Review != "pending" {
		return chargeInvalid("Receive the video before checking it")
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
	if in.Version == 0 && in.Mark != nil {
		created, e := tx.Exec(ctx, `INSERT INTO weekly_pti_rows(week_start,truck_id,data)
 SELECT w.week_start,t.id,jsonb_build_object('weekStart',w.week_start::text,'truckId',t.id::text,'truckUnit',t.unit_number,'crew','[]'::jsonb,'eligibility','unknown','snapshotEligibility','unknown','review','pending','received',false,'notes','')
 FROM weekly_pti_weeks w CROSS JOIN trucks t WHERE w.week_start=$1 AND t.id=$2
 ON CONFLICT DO NOTHING`, week, truck)
		if e != nil {
			return row, e
		}
		if created.RowsAffected() == 1 {
			in.Version = 1
		}
	}
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
	if in.UndoRequestID != "" {
		var beforeJSON, afterJSON []byte
		err = tx.QueryRow(ctx, `SELECT before_data,after_data FROM weekly_pti_events WHERE id::text=$1 AND week_start=$2 AND truck_id=$3 AND actor_id=$4`, in.UndoRequestID, week, truck, actor).Scan(&beforeJSON, &afterJSON)
		if errors.Is(err, pgx.ErrNoRows) {
			return row, ErrPTIConflict
		}
		if err != nil {
			return row, err
		}
		var previous, applied PTIRow
		if err = json.Unmarshal(beforeJSON, &previous); err != nil {
			return row, err
		}
		if err = json.Unmarshal(afterJSON, &applied); err != nil {
			return row, err
		}
		marksEqual := (row.Mark == nil && applied.Mark == nil) || (row.Mark != nil && applied.Mark != nil && *row.Mark == *applied.Mark)
		if !marksEqual || row.Received != applied.Received || row.Review != applied.Review {
			return row, ErrPTIConflict
		}
		in.Mark, in.Received, in.Review = previous.Mark, previous.Received, previous.Review
		in.Notes = row.Notes
	}
	// Legacy fields remain decodable, but eligibility is server-owned and links archived.
	if (in.Eligibility != "" && in.Eligibility != row.Eligibility) || (in.VideoURL != "" && in.VideoURL != row.VideoURL) {
		return row, chargeInvalid("PTI eligibility is automatic; video links are no longer editable")
	}
	if row.Mark != nil && in.Mark == nil && in.UndoRequestID == "" {
		return row, chargeInvalid("Reload PTI and choose /, 1 or 0")
	}
	if in.Mark != nil && *in.Mark == "" {
		in.Mark = nil
	}
	sameMark := (row.Mark == nil && in.Mark == nil) || (row.Mark != nil && in.Mark != nil && *row.Mark == *in.Mark)
	if sameMark && row.Received == in.Received && row.Review == in.Review && row.Notes == in.Notes {
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
	if row.Review != review {
		row.ReviewedAt = nil
		row.ReviewedBy = ""
		if review != "pending" {
			row.ReviewedAt = &now
			row.ReviewedBy = author
		}
	}
	row.Received = in.Received
	row.Mark = in.Mark
	row.Review = review
	row.Notes = in.Notes
	row.UpdatedAt = &now
	row.UpdatedBy = author
	row.Version++
	after, _ := json.Marshal(row)
	beforeRaw, _ := json.Marshal(before)
	if _, err = tx.Exec(ctx, `WITH changed AS (
 UPDATE weekly_pti_rows SET version=$3,data=$4 WHERE week_start=$1 AND truck_id=$2 RETURNING week_start,truck_id,data
 )
 INSERT INTO weekly_pti_events(id,request_hash,week_start,truck_id,before_data,after_data,actor_id,actor_name)
 SELECT $5,$6,week_start,truck_id,$7,data,$8,$9 FROM changed`, week, truck, row.Version, after, in.RequestID, hash, beforeRaw, actor, author); err != nil {
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
