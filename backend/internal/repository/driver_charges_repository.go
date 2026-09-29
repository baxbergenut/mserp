package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type DriverChargeRepository struct{ pool *pgxpool.Pool }

func NewDriverChargeRepository(pool *pgxpool.Pool) *DriverChargeRepository {
	return &DriverChargeRepository{pool}
}

type chargeQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func chargeData(ctx context.Context, q chargeQuery, driver string) (ChargeData, map[string]map[string]bool, error) {
	data := ChargeData{Types: []ChargeType{}, Schedules: []ChargeSchedule{}, CurrentWeek: ChargeCurrentWeek()}
	loads := map[string]map[string]bool{}
	rows, err := q.Query(ctx, `SELECT id::text,name,direction,amount::text,archived,version FROM driver_charge_types ORDER BY name,id`)
	if err != nil {
		return data, loads, err
	}
	for rows.Next() {
		var t ChargeType
		if err = rows.Scan(&t.ID, &t.Name, &t.Direction, &t.Amount, &t.Archived, &t.Version); err != nil {
			rows.Close()
			return data, loads, err
		}
		data.Types = append(data.Types, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, loads, err
	}
	rows, err = q.Query(ctx, `SELECT s.id::text,s.driver_id::text,d.full_name,s.type_id::text,s.kind,s.name,s.direction,s.start_week::text,s.end_week::text,s.eligibility,s.total::text,s.version,s.installment_count FROM driver_charge_schedules s JOIN drivers d ON d.id=s.driver_id WHERE ($1='' OR s.driver_id::text=$1) ORDER BY d.full_name,s.name,s.id`, driver)
	if err != nil {
		return data, loads, err
	}
	index := map[string]int{}
	for rows.Next() {
		var s ChargeSchedule
		if err = rows.Scan(&s.ID, &s.DriverID, &s.DriverName, &s.TypeID, &s.Kind, &s.Name, &s.Direction, &s.StartWeek, &s.EndWeek, &s.Eligibility, &s.Total, &s.Version, &s.InstallmentCount); err != nil {
			rows.Close()
			return data, loads, err
		}
		s.Phases = []ChargePhase{}
		s.Occurrences = []ChargeOccurrence{}
		index[s.ID] = len(data.Schedules)
		data.Schedules = append(data.Schedules, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, loads, err
	}
	if len(data.Schedules) == 0 {
		return data, loads, nil
	}
	rows, err = q.Query(ctx, `SELECT p.schedule_id::text,p.week_start::text,p.amount::text,p.paused FROM driver_charge_phases p JOIN driver_charge_schedules s ON s.id=p.schedule_id WHERE ($1='' OR s.driver_id::text=$1) ORDER BY p.week_start`, driver)
	if err != nil {
		return data, loads, err
	}
	for rows.Next() {
		var id string
		var p ChargePhase
		if err = rows.Scan(&id, &p.WeekStart, &p.Amount, &p.Paused); err != nil {
			rows.Close()
			return data, loads, err
		}
		i := index[id]
		data.Schedules[i].Phases = append(data.Schedules[i].Phases, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, loads, err
	}
	rows, err = q.Query(ctx, `SELECT o.schedule_id::text,o.week_start::text,o.name,o.scheduled_amount::text,o.amount::text,o.overridden,o.confirmed_at,o.confirmed_by::text,o.version FROM driver_charge_occurrences o JOIN driver_charge_schedules s ON s.id=o.schedule_id WHERE ($1='' OR s.driver_id::text=$1) ORDER BY o.week_start`, driver)
	if err != nil {
		return data, loads, err
	}
	for rows.Next() {
		var o ChargeOccurrence
		if err = rows.Scan(&o.ScheduleID, &o.WeekStart, &o.Name, &o.ScheduledAmount, &o.Amount, &o.Overridden, &o.ConfirmedAt, &o.ConfirmedBy, &o.Version); err != nil {
			rows.Close()
			return data, loads, err
		}
		i := index[o.ScheduleID]
		o.Kind = data.Schedules[i].Kind
		o.ScheduleVersion = data.Schedules[i].Version
		data.Schedules[i].Occurrences = append(data.Schedules[i].Occurrences, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, loads, err
	}
	rows, err = q.Query(ctx, `SELECT DISTINCT e.driver_id::text,date_trunc('week',e.service_date)::date::text FROM `+grossBoardEntriesSQL+` e WHERE NOT e.deleted AND btrim(e.load_number)<>'' AND e.day_status='' AND ($1='' OR e.driver_id::text=$1) AND EXISTS(SELECT 1 FROM driver_charge_schedules s WHERE s.driver_id=e.driver_id)`, driver)
	if err != nil {
		return data, loads, err
	}
	for rows.Next() {
		var id, w string
		if err = rows.Scan(&id, &w); err != nil {
			rows.Close()
			return data, loads, err
		}
		if loads[id] == nil {
			loads[id] = map[string]bool{}
		}
		loads[id][w] = true
	}
	err = rows.Err()
	rows.Close()
	return data, loads, err
}
func (r *DriverChargeRepository) List(ctx context.Context, driver string) (ChargeData, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ChargeData{}, err
	}
	defer tx.Rollback(ctx)
	data, loads, err := chargeData(ctx, tx, driver)
	if err != nil {
		return data, err
	}
	for i := range data.Schedules {
		if err = summarizeCharge(&data.Schedules[i], loads[data.Schedules[i].DriverID]); err != nil {
			return data, err
		}
	}
	return data, tx.Commit(ctx)
}
func chargeAudit(ctx context.Context, tx pgx.Tx, schedule, typeID, actor, action string, detail any) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_charge_events(schedule_id,type_id,actor_id,action,details) VALUES(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,$5)`, schedule, typeID, actor, action, b)
	return err
}
func (r *DriverChargeRepository) SaveType(ctx context.Context, t ChargeType, actor string) (ChargeType, error) {
	t.Name = strings.TrimSpace(t.Name)
	n, err := chargeCents(t.Amount)
	if err != nil || n <= 0 || len([]rune(t.Name)) < 1 || len([]rune(t.Name)) > 200 || (t.Direction != "charge" && t.Direction != "reimbursement") {
		return t, chargeInvalid("Provide a name, direction and positive amount")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return t, err
	}
	defer tx.Rollback(ctx)
	if t.ID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO driver_charge_types(name,direction,amount,archived) VALUES($1,$2,$3::numeric,$4) RETURNING id::text,version`, t.Name, t.Direction, t.Amount, t.Archived).Scan(&t.ID, &t.Version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE driver_charge_types SET name=$2,direction=$3,amount=$4::numeric,archived=$5,version=version+1 WHERE id=$1 AND version=$6 RETURNING version`, t.ID, t.Name, t.Direction, t.Amount, t.Archived, t.Version).Scan(&t.Version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrChargeConflict
	}
	if err != nil {
		return t, err
	}
	if err = chargeAudit(ctx, tx, "", t.ID, actor, "type_saved", t); err != nil {
		return t, err
	}
	return t, tx.Commit(ctx)
}
func lockChargeDrivers(ctx context.Context, tx pgx.Tx, ids []string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM drivers WHERE id::text=ANY($1::text[]) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return err
	}
	n := 0
	for rows.Next() {
		n++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if n != len(ids) {
		return chargeInvalid("Select distinct existing drivers")
	}
	return nil
}
func validateChargeCreate(c *ChargeCreate) error {
	if c.Installments < 0 || c.Installments > 5200 || (c.Kind == "recurring" && c.Installments != 0) {
		return chargeInvalid("Invalid installment count")
	}
	if c.Kind != "recurring" && c.Kind != "installment" {
		return chargeInvalid("Invalid charge kind")
	}
	if c.Eligibility != "calendar" && c.Eligibility != "loads" {
		return chargeInvalid("Choose calendar or load weeks")
	}
	if _, err := chargeWeek(c.StartWeek); err != nil {
		return err
	}
	if c.StartWeek < ChargeCurrentWeek() {
		return chargeInvalid("New schedules must start in the current or a future week")
	}
	if c.EndWeek != nil {
		if _, err := chargeWeek(*c.EndWeek); err != nil {
			return err
		}
		if *c.EndWeek < c.StartWeek {
			return chargeInvalid("Final week must follow the start week")
		}
	}
	if c.Kind == "installment" {
		total, err := chargeCents(c.Total)
		if err != nil || total <= 0 {
			return chargeInvalid("Provide a positive total charge")
		}
		if c.Installments > 0 {
			if c.Installments > 5200 || int64(c.Installments) > total {
				return chargeInvalid("Choose up to 5,200 installments, at least one cent each")
			}
			c.Amount = chargeMoney(total / int64(c.Installments))
		}
		if len(c.DriverIDs) != 1 {
			return chargeInvalid("An installment plan belongs to one driver")
		}
		c.Name = strings.TrimSpace(c.Name)
		if len([]rune(c.Name)) < 1 || len([]rune(c.Name)) > 200 {
			return chargeInvalid("Provide a description up to 200 characters")
		}
	}
	n, err := chargeCents(c.Amount)
	if err != nil || n <= 0 {
		return chargeInvalid("Provide a positive weekly amount")
	}
	if len(c.DriverIDs) < 1 || len(c.DriverIDs) > 100 {
		return chargeInvalid("Select 1–100 drivers")
	}
	return nil
}
func (r *DriverChargeRepository) Create(ctx context.Context, c ChargeCreate, actor string) ([]string, error) {
	if err := validateChargeCreate(&c); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockChargeDrivers(ctx, tx, c.DriverIDs); err != nil {
		return nil, err
	}
	direction := "charge"
	var typeID *string
	if c.Kind == "recurring" {
		var archived bool
		err = tx.QueryRow(ctx, `SELECT name,direction,archived FROM driver_charge_types WHERE id=$1 FOR SHARE`, c.TypeID).Scan(&c.Name, &direction, &archived)
		if err != nil {
			return nil, err
		}
		if archived {
			return nil, chargeInvalid("This charge type is archived")
		}
		typeID = &c.TypeID
	}
	ids := []string{}
	for _, driver := range c.DriverIDs {
		var active bool
		if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1`, driver).Scan(&active); err != nil {
			return nil, err
		}
		if !active {
			return nil, chargeInvalid("Reactivate this driver before assigning new charges")
		}
		if typeID != nil {
			var overlaps bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_charge_schedules WHERE driver_id=$1 AND type_id=$2 AND start_week<=COALESCE($4::date,'infinity'::date) AND COALESCE(end_week,'infinity'::date)>=$3::date)`, driver, *typeID, c.StartWeek, c.EndWeek).Scan(&overlaps)
			if err != nil {
				return nil, err
			}
			if overlaps {
				return nil, chargeInvalid("This driver already has an overlapping assignment for this charge type")
			}
		}
		var total *string
		if c.Kind == "installment" {
			total = &c.Total
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO driver_charge_schedules(driver_id,type_id,kind,name,direction,start_week,end_week,eligibility,total,installment_count) VALUES($1,$2,$3,$4,$5,$6::date,$7::date,$8,$9::numeric,$10) RETURNING id::text`, driver, typeID, c.Kind, c.Name, direction, c.StartWeek, c.EndWeek, c.Eligibility, total, c.Installments).Scan(&id)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES($1,$2::date,$3::numeric)`, id, c.StartWeek, c.Amount); err != nil {
			return nil, err
		}
		if err = chargeAudit(ctx, tx, id, "", actor, "created", c); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, tx.Commit(ctx)
}
func storeOccurrence(ctx context.Context, tx pgx.Tx, o ChargeOccurrence) error {
	_, err := tx.Exec(ctx, `INSERT INTO driver_charge_occurrences(schedule_id,week_start,name,scheduled_amount,amount,overridden,confirmed_at,confirmed_by) VALUES($1,$2::date,$3,$4::numeric,$5::numeric,$6,$7,$8::uuid) ON CONFLICT(schedule_id,week_start) DO UPDATE SET name=EXCLUDED.name,scheduled_amount=EXCLUDED.scheduled_amount,amount=EXCLUDED.amount,overridden=EXCLUDED.overridden,confirmed_at=EXCLUDED.confirmed_at,confirmed_by=EXCLUDED.confirmed_by,version=driver_charge_occurrences.version+1`, o.ScheduleID, o.WeekStart, o.Name, o.ScheduledAmount, o.Amount, o.Overridden, o.ConfirmedAt, o.ConfirmedBy)
	return err
}

// Freeze prior automatic rows on writes, not reads, so later schedule changes
// cannot move an overdue installment or a historical charge to a different week.
func freezeChargesBefore(ctx context.Context, tx pgx.Tx, s *ChargeSchedule, week string, loads map[string]bool) error {
	d, _ := chargeWeek(week)
	rows, err := projectCharges(*s, d.AddDate(0, 0, -7).Format(time.DateOnly), loads, false)
	if err != nil {
		return err
	}
	for _, o := range rows {
		if o.Version == 0 {
			if err = storeOccurrence(ctx, tx, o); err != nil {
				return err
			}
			o.Version = 1
			s.Occurrences = append(s.Occurrences, o)
		}
	}
	return nil
}
func (r *DriverChargeRepository) Bulk(ctx context.Context, b ChargeBulk, actor string) error {
	if _, err := chargeWeek(b.WeekStart); err != nil {
		return err
	}
	if b.WeekStart < ChargeCurrentWeek() {
		return chargeInvalid("Bulk changes cannot change previous weeks")
	}
	if len(b.Targets) == 0 || len(b.Targets) > 100 {
		return chargeInvalid("Select 1–100 assignments")
	}
	if b.Action != "amount" && b.Action != "pause" && b.Action != "resume" && b.Action != "end" {
		return chargeInvalid("Invalid bulk action")
	}
	if b.Action == "amount" {
		n, e := chargeCents(b.Amount)
		if e != nil || n <= 0 {
			return chargeInvalid("Provide a positive amount")
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ids := []string{}
	for _, v := range b.Targets {
		ids = append(ids, v.ID)
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT driver_id::text FROM driver_charge_schedules WHERE id::text=ANY($1::text[]) ORDER BY driver_id::text`, ids)
	if err != nil {
		return err
	}
	drivers := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		drivers = append(drivers, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = lockChargeDrivers(ctx, tx, drivers); err != nil {
		return err
	}
	data, loads, err := chargeData(ctx, tx, "")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, target := range b.Targets {
		if seen[target.ID] {
			return chargeInvalid("Duplicate assignment")
		}
		seen[target.ID] = true
		var s *ChargeSchedule
		for i := range data.Schedules {
			if data.Schedules[i].ID == target.ID {
				s = &data.Schedules[i]
				break
			}
		}
		if s == nil || s.Version != target.Version {
			return ErrChargeConflict
		}
		if b.WeekStart < s.StartWeek {
			return chargeInvalid("Effective week cannot precede the assignment")
		}
		if s.EndWeek != nil && b.WeekStart > *s.EndWeek {
			return chargeInvalid("This assignment has ended")
		}
		if b.Action == "end" && s.Kind != "recurring" {
			return chargeInvalid("Pause an installment plan to retain its outstanding balance")
		}
		if err = freezeChargesBefore(ctx, tx, s, b.WeekStart, loads[s.DriverID]); err != nil {
			return err
		}
		for _, o := range s.Occurrences {
			if o.WeekStart >= b.WeekStart && (o.Overridden || o.ConfirmedAt != nil) {
				return chargeInvalid("Correct the saved charge in week %s before changing this schedule", o.WeekStart)
			}
		}
		// A newly selected effective change replaces future phases, after preview.
		if _, err = tx.Exec(ctx, `DELETE FROM driver_charge_phases WHERE schedule_id=$1 AND week_start >= $2::date`, s.ID, b.WeekStart); err != nil {
			return err
		}
		p := phaseAt(*s, b.WeekStart)
		p.WeekStart = b.WeekStart
		switch b.Action {
		case "amount":
			p.Amount = b.Amount
		case "pause", "end":
			p.Paused = true
		case "resume":
			p.Paused = false
		}
		if b.Action == "resume" {
			var active bool
			if err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1`, s.DriverID).Scan(&active); err != nil {
				return err
			}
			if !active {
				return chargeInvalid("Reactivate this driver before resuming charges")
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount,paused) VALUES($1,$2::date,$3::numeric,$4)`, s.ID, p.WeekStart, p.Amount, p.Paused); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM driver_charge_occurrences WHERE schedule_id=$1 AND week_start >= $2::date AND NOT overridden AND confirmed_at IS NULL`, s.ID, b.WeekStart); err != nil {
			return err
		}
		if b.Action == "end" { // effective week is the first week with no charge
			d, _ := chargeWeek(b.WeekStart)
			end := d.AddDate(0, 0, -7).Format(time.DateOnly)
			_, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET end_week=$2::date,version=version+1 WHERE id=$1`, s.ID, end)
		} else {
			_, err = tx.Exec(ctx, `UPDATE driver_charge_schedules SET version=version+1 WHERE id=$1`, s.ID)
		}
		if err != nil {
			return err
		}
		if err = chargeAudit(ctx, tx, s.ID, "", actor, b.Action, b); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *DriverChargeRepository) History(ctx context.Context, id string) ([]ChargeEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT e.id,e.action,coalesce(u.username,'System'),e.details,e.created_at FROM driver_charge_events e LEFT JOIN app_users u ON u.id=e.actor_id WHERE e.schedule_id=$1 ORDER BY e.id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChargeEvent{}
	for rows.Next() {
		var e ChargeEvent
		var raw []byte
		if err = rows.Scan(&e.ID, &e.Action, &e.Actor, &raw, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r *DriverChargeRepository) Preview(ctx context.Context, id string) ([]ChargeOccurrence, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	data, loads, err := chargeData(ctx, tx, "")
	if err != nil {
		return nil, err
	}
	for _, s := range data.Schedules {
		if s.ID == id {
			rows, err := projectCharges(s, "2100-12-27", loads[s.DriverID], true)
			if err != nil {
				return nil, err
			}
			return rows, tx.Commit(ctx)
		}
	}
	return nil, ErrNotFound
}

func (r *DriverChargeRepository) Draft(ctx context.Context, c ChargeCreate) ([]ChargeOccurrence, error) {
	if err := validateChargeCreate(&c); err != nil {
		return nil, err
	}
	s := ChargeSchedule{ID: "preview", InstallmentCount: c.Installments, Kind: c.Kind, Name: c.Name, StartWeek: c.StartWeek, EndWeek: c.EndWeek, Eligibility: c.Eligibility, Direction: "charge", Phases: []ChargePhase{{WeekStart: c.StartWeek, Amount: c.Amount}}}
	if c.Kind == "installment" {
		s.Total = &c.Total
	} else {
		var archived bool
		if err := r.pool.QueryRow(ctx, `SELECT name,direction,archived FROM driver_charge_types WHERE id=$1`, c.TypeID).Scan(&s.Name, &s.Direction, &archived); err != nil {
			return nil, err
		}
		if archived {
			return nil, chargeInvalid("This charge type is archived")
		}
	}
	through := "2100-12-27"
	if c.Kind == "recurring" {
		d, _ := chargeWeek(c.StartWeek)
		through = d.AddDate(0, 0, 7*11).Format("2006-01-02")
	}
	return projectCharges(s, through, nil, true)
}
