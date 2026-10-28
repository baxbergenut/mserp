package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDispatcherPayConflict = errors.New("Dispatcher pay changed. Reload and review before saving again.")

type DispatcherPayRepository struct{ pool *pgxpool.Pool }

func NewDispatcherPayRepository(pool *pgxpool.Pool) *DispatcherPayRepository {
	return &DispatcherPayRepository{pool}
}

type CommissionBracket struct {
	MinRPM  float64 `json:"minRPM"`
	Percent float64 `json:"percent"`
}
type CommissionTier struct {
	MinGross float64             `json:"minGross"`
	Brackets []CommissionBracket `json:"brackets"`
}
type CommissionRules struct {
	HomeDayCredit    float64          `json:"homeDayCredit"`
	MaxTierPromotion int              `json:"maxTierPromotion"`
	Tiers            []CommissionTier `json:"tiers"`
}
type CommissionRuleVersion struct {
	ID            int64           `json:"id"`
	EffectiveWeek string          `json:"effectiveWeek"`
	Rules         CommissionRules `json:"rules"`
	CreatedAt     time.Time       `json:"createdAt"`
}
type CommissionRuleChange struct {
	ExpectedRevision int64           `json:"expectedRevision"`
	EffectiveWeek    string          `json:"effectiveWeek"`
	Rules            CommissionRules `json:"rules"`
}
type DispatcherPayInput struct {
	DriverID string `json:"driverId"`
	HomeDays int    `json:"homeDays"`
	Note     string `json:"note"`
	Version  int    `json:"version"`
}
type DispatcherCommissionRow struct {
	DispatcherPayInput
	DriverName    string   `json:"driverName"`
	TruckUnits    []string `json:"truckUnits"`
	Gross         float64  `json:"gross"`
	Miles         float64  `json:"miles"`
	RPM           float64  `json:"rpm"`
	CreditedGross float64  `json:"creditedGross"`
	CreditedMiles *float64 `json:"creditedMiles,omitempty"`
	AdjustedGross float64  `json:"adjustedGross"`
	ActualTier    float64  `json:"actualTier"`
	SelectedTier  float64  `json:"selectedTier"`
	Rate          float64  `json:"rate"`
	Pay           float64  `json:"pay"`
	LoadCount     int      `json:"loadCount"`
	Issues        []string `json:"issues"`
}
type DispatcherCommissionStatement struct {
	DispatcherID   string                    `json:"dispatcherId"`
	DispatcherName string                    `json:"dispatcherName"`
	Rows           []DispatcherCommissionRow `json:"rows"`
	Gross          float64                   `json:"gross"`
	Pay            float64                   `json:"pay"`
	LoadCount      int                       `json:"loadCount"`
	Rule           *CommissionRuleVersion    `json:"rule"`
	Revision       string                    `json:"revision"`
	Finalized      bool                      `json:"finalized"`
	SourceChanged  bool                      `json:"sourceChanged"`
	Version        int                       `json:"version"`
	FinalizedAt    *time.Time                `json:"finalizedAt,omitempty"`
}
type DispatcherCommissionReport struct {
	WeekStart      string                          `json:"weekStart"`
	AvailableWeeks []string                        `json:"availableWeeks"`
	Rule           *CommissionRuleVersion          `json:"rule"`
	Statements     []DispatcherCommissionStatement `json:"statements"`
}
type DispatcherStatementChange struct {
	DispatcherID string `json:"dispatcherId"`
	Revision     string `json:"revision"`
	Version      int    `json:"version"`
	Reason       string `json:"reason"`
}

func dispatcherWeek(value string) (time.Time, error) {
	w, e := time.Parse(time.DateOnly, value)
	if e != nil || w.Year() < 2000 || w.Year() > 2100 || w.Weekday() != time.Monday {
		return w, chargeInvalid("Choose a Monday between 2000 and 2100")
	}
	return w, nil
}
func commissionNumber(v float64, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= max && math.Abs(v*100-math.Round(v*100)) < 0.000001
}
func (r CommissionRules) Validate() error {
	if !commissionNumber(r.HomeDayCredit, 1000000) || r.MaxTierPromotion < 0 || r.MaxTierPromotion > len(r.Tiers) || len(r.Tiers) < 1 || len(r.Tiers) > 10 {
		return chargeInvalid("Enter valid home-day credit, promotion limit and 1–10 gross tiers")
	}
	previous := -1.0
	for _, t := range r.Tiers {
		if !commissionNumber(t.MinGross, 100000000) || t.MinGross <= 0 || t.MinGross <= previous || len(t.Brackets) < 1 || len(t.Brackets) > 20 {
			return chargeInvalid("Gross thresholds must increase and each tier needs 1–20 RPM brackets")
		}
		previous = t.MinGross
		rpm := -1.0
		for _, b := range t.Brackets {
			if !commissionNumber(b.MinRPM, 1000) || b.MinRPM <= rpm || !commissionNumber(b.Percent, 100) {
				return chargeInvalid("RPM thresholds must increase; percentages must be between 0 and 100, with at most two decimals")
			}
			rpm = b.MinRPM
		}
	}
	return nil
}

func readCommissionRules(ctx context.Context, tx pgx.Tx) ([]CommissionRuleVersion, error) {
	rows, err := tx.Query(ctx, `SELECT id,effective_week::text,rules,created_at FROM dispatcher_commission_rules ORDER BY effective_week DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CommissionRuleVersion{}
	for rows.Next() {
		var v CommissionRuleVersion
		var raw []byte
		if err = rows.Scan(&v.ID, &v.EffectiveWeek, &raw, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Rules); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *DispatcherPayRepository) Rules(ctx context.Context) ([]CommissionRuleVersion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	out, err := readCommissionRules(ctx, tx)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
func (r *DispatcherPayRepository) SaveRules(ctx context.Context, in CommissionRuleChange, actor string) error {
	if _, err := dispatcherWeek(in.EffectiveWeek); err != nil {
		return err
	}
	if err := in.Rules.Validate(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	var latest int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM dispatcher_commission_rules`).Scan(&latest); err != nil {
		return err
	}
	if latest != in.ExpectedRevision {
		return ErrDispatcherPayConflict
	}
	raw, err := json.Marshal(in.Rules)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO dispatcher_commission_rules(effective_week,rules,actor_id) VALUES($1::date,$2,$3)`, in.EffectiveWeek, raw, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Integer cents and hundredths of a mile keep bracket boundaries exact.
func commissionCalculation(gross, miles int64, home int, rules CommissionRules) (adjusted int64, actual, selected int, rate int64) {
	adjusted = gross + int64(home)*int64(math.Round(rules.HomeDayCredit*100))
	raw := 0
	for i, t := range rules.Tiers {
		threshold := int64(math.Round(t.MinGross * 100))
		if gross >= threshold {
			actual = i + 1
		}
		if adjusted >= threshold {
			raw = i + 1
		}
	}
	selected = min(raw, actual+rules.MaxTierPromotion)
	if selected > 0 && miles > 0 {
		for _, b := range rules.Tiers[selected-1].Brackets {
			if gross*100 >= int64(math.Round(b.MinRPM*100))*miles {
				rate = int64(math.Round(b.Percent * 100))
			}
		}
	}
	return
}
func commissionMoney(cents int64) float64 { return float64(cents) / 100 }
func roundedCommission(gross, rate int64) int64 {
	value := gross * rate
	if value < 0 {
		return -((-value + 5000) / 10000)
	}
	return (value + 5000) / 10000
}
func commissionHash(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func appendIssue(issues []string, value string) []string {
	for _, v := range issues {
		if v == value {
			return issues
		}
	}
	return append(issues, value)
}

// Match the workbook's driver-week calculation, then allocate actual gross to
// Gross Board's dated dispatcher credit. A midweek reassignment never duplicates it.
func buildDispatcherCommissions(board GrossBoard, inputs map[string]DispatcherPayInput, rule *CommissionRuleVersion) []DispatcherCommissionStatement {
	type bucket struct {
		gross, miles int64
		issues       []string
		units        map[string]bool
		counts       map[string]int
		credit       map[string]int64
		creditMiles  map[string]int64
		names        map[string]string
	}
	totals := map[string]*bucket{}
	drivers := map[string]GrossBoardDriver{}
	for _, d := range board.Drivers {
		drivers[d.ID] = d
		totals[d.ID] = &bucket{units: map[string]bool{}, counts: map[string]int{}, credit: map[string]int64{}, creditMiles: map[string]int64{}, names: map[string]string{}}
	}
	for _, e := range board.Entries {
		if e.Deleted || e.Duplicate || e.TransferOut {
			continue
		}
		b, ok := totals[e.DriverID]
		if !ok {
			continue
		}
		d := drivers[e.DriverID]
		if e.DayStatus != "" || (strings.TrimSpace(e.LoadNumber) == "" && e.OriginalRate == "" && e.Miles == "") {
			continue
		}
		if strings.TrimSpace(e.LoadNumber) == "" {
			b.issues = appendIssue(b.issues, "Enter the missing Gross Board load number")
		}
		dispatcher, name, unit := "", "Unassigned", ""
		for _, a := range d.Assignments {
			if a.Date == e.Date {
				dispatcher, name, unit = a.DispatcherID, a.DispatcherName, a.TruckUnit
				break
			}
		}
		if e.TransferVersion > 0 {
			dispatcher, name = e.CreditedDispatcherID, e.CreditedDispatcherName
		}
		if name == "" {
			name = "Unassigned"
		}
		b.names[dispatcher] = name
		b.counts[dispatcher]++
		if unit != "" {
			b.units[unit] = true
		}
		g, ge := chargeCents(e.OriginalRate)
		m, me := chargeCents(e.Miles)
		if e.TripReview {
			b.issues = appendIssue(b.issues, "Resolve split-trip selection in Gross Board")
		}
		if ge != nil {
			b.issues = appendIssue(b.issues, "Missing or invalid Gross Board total gross")
			g = 0
		}
		if me != nil {
			b.issues = appendIssue(b.issues, "Missing or invalid Gross Board miles")
			m = 0
		}
		b.gross += g
		b.miles += m
		b.credit[dispatcher] += g
		b.creditMiles[dispatcher] += m
	}
	statements := map[string]*DispatcherCommissionStatement{}
	for id, b := range totals {
		if len(b.counts) == 0 {
			continue
		}
		d := drivers[id]
		in := inputs[id]
		in.DriverID = id
		row := DispatcherCommissionRow{DispatcherPayInput: in, DriverName: d.FullName, TruckUnits: []string{}, Gross: commissionMoney(b.gross), Miles: commissionMoney(b.miles), Issues: append([]string{}, b.issues...)}
		if b.miles > 0 {
			row.RPM = float64(b.gross) / float64(b.miles)
		} else {
			row.Issues = appendIssue(row.Issues, "Weekly miles must be greater than zero")
		}
		for unit := range b.units {
			row.TruckUnits = append(row.TruckUnits, unit)
		}
		sort.Strings(row.TruckUnits)
		if rule != nil {
			adjusted, actual, selected, rate := commissionCalculation(b.gross, b.miles, in.HomeDays, rule.Rules)
			row.AdjustedGross = commissionMoney(adjusted)
			row.Rate = float64(rate) / 100
			if actual > 0 {
				row.ActualTier = rule.Rules.Tiers[actual-1].MinGross
			}
			if selected > 0 {
				row.SelectedTier = rule.Rules.Tiers[selected-1].MinGross
			}
		} else {
			row.Issues = appendIssue(row.Issues, "No commission rules apply to this week")
		}
		for dispatcher, count := range b.counts {
			s := statements[dispatcher]
			if s == nil {
				s = &DispatcherCommissionStatement{DispatcherID: dispatcher, DispatcherName: b.names[dispatcher], Rule: rule, Rows: []DispatcherCommissionRow{}}
				statements[dispatcher] = s
			}
			part := row
			part.CreditedGross = commissionMoney(b.credit[dispatcher])
			miles := commissionMoney(b.creditMiles[dispatcher])
			part.CreditedMiles = &miles
			part.LoadCount = count
			if len(part.Issues) == 0 && dispatcher != "" {
				part.Pay = commissionMoney(roundedCommission(b.credit[dispatcher], int64(math.Round(part.Rate*100))))
			}
			s.Rows = append(s.Rows, part)
			s.LoadCount += count
		}
	}
	out := []DispatcherCommissionStatement{}
	for _, s := range statements {
		sort.Slice(s.Rows, func(i, j int) bool {
			if s.Rows[i].DriverName == s.Rows[j].DriverName {
				return s.Rows[i].DriverID < s.Rows[j].DriverID
			}
			return s.Rows[i].DriverName < s.Rows[j].DriverName
		})
		var gross, pay int64
		for _, row := range s.Rows {
			gross += int64(math.Round(row.CreditedGross * 100))
			pay += int64(math.Round(row.Pay * 100))
		}
		s.Gross = commissionMoney(gross)
		s.Pay = commissionMoney(pay)
		// Per-dispatcher mileage is display detail added after the original
		// snapshot contract. Keep existing financial revision hashes comparable.
		revision := *s
		revision.Rows = append([]DispatcherCommissionRow{}, s.Rows...)
		for i := range revision.Rows {
			revision.Rows[i].CreditedMiles = nil
		}
		s.Revision = commissionHash(struct {
			Week      string
			Statement *DispatcherCommissionStatement
		}{board.WeekStart, &revision})
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DispatcherName == out[j].DispatcherName {
			return out[i].DispatcherID < out[j].DispatcherID
		}
		return out[i].DispatcherName < out[j].DispatcherName
	})
	return out
}

func (r *DispatcherPayRepository) report(ctx context.Context, tx pgx.Tx, week time.Time, frozen bool) (DispatcherCommissionReport, error) {
	out := DispatcherCommissionReport{WeekStart: week.Format(time.DateOnly), AvailableWeeks: []string{}, Statements: []DispatcherCommissionStatement{}}
	versions, err := readCommissionRules(ctx, tx)
	if err != nil {
		return out, err
	}
	for _, v := range versions {
		if v.EffectiveWeek <= out.WeekStart {
			out.Rule = &v
			break
		}
	}
	board, err := readGrossBoard(ctx, tx, week)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT driver_id::text,home_days,note,version FROM dispatcher_pay_inputs WHERE week_start=$1`, week)
	if err != nil {
		return out, err
	}
	inputs := map[string]DispatcherPayInput{}
	for rows.Next() {
		var v DispatcherPayInput
		if err = rows.Scan(&v.DriverID, &v.HomeDays, &v.Note, &v.Version); err != nil {
			rows.Close()
			return out, err
		}
		inputs[v.DriverID] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.Statements = buildDispatcherCommissions(board, inputs, out.Rule)
	rows, err = tx.Query(ctx, `SELECT dispatcher_id,finalized,report,version,updated_at FROM dispatcher_pay_statements WHERE week_start=$1 ORDER BY dispatcher_id`, week)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		var closed bool
		var raw []byte
		var version int
		var at time.Time
		if err = rows.Scan(&id, &closed, &raw, &version, &at); err != nil {
			rows.Close()
			return out, err
		}
		index := -1
		for i := range out.Statements {
			if out.Statements[i].DispatcherID == id {
				index = i
				out.Statements[i].Version = version
				break
			}
		}
		if closed && frozen {
			var saved DispatcherCommissionStatement
			if err = json.Unmarshal(raw, &saved); err != nil {
				rows.Close()
				return out, err
			}
			saved.Finalized = true
			saved.Version = version
			saved.FinalizedAt = &at
			saved.SourceChanged = index < 0 || saved.Revision != out.Statements[index].Revision
			if index < 0 {
				out.Statements = append(out.Statements, saved)
			} else {
				out.Statements[index] = saved
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT date_trunc('week',day)::date::text FROM (
 SELECT service_date AS day FROM `+grossBoardEntriesSQL+` WHERE NOT deleted UNION ALL SELECT dispatch_date FROM gross_board_transfers WHERE active UNION ALL SELECT week_start FROM dispatcher_pay_statements UNION ALL SELECT $1::date) days ORDER BY 1 DESC`, week)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var w string
		if err = rows.Scan(&w); err != nil {
			return out, err
		}
		out.AvailableWeeks = append(out.AvailableWeeks, w)
	}
	return out, rows.Err()
}
func (r *DispatcherPayRepository) Get(ctx context.Context, week string) (DispatcherCommissionReport, error) {
	w, err := dispatcherWeek(week)
	if err != nil {
		return DispatcherCommissionReport{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DispatcherCommissionReport{}, err
	}
	defer tx.Rollback(ctx)
	out, err := r.report(ctx, tx, w, true)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *DispatcherPayRepository) SaveInput(ctx context.Context, week string, in DispatcherPayInput, actor string) error {
	w, err := dispatcherWeek(week)
	if err != nil {
		return err
	}
	in.Note = strings.TrimSpace(in.Note)
	if in.HomeDays < 0 || in.HomeDays > 7 || len([]rune(in.Note)) > 1000 || in.Version < 0 {
		return chargeInvalid("Home days must be 0–7 and comments at most 1,000 characters")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	var closed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dispatcher_pay_statements s CROSS JOIN LATERAL jsonb_array_elements(s.report->'rows') row WHERE s.week_start=$1 AND s.finalized AND row->>'driverId'=$2)`, w, in.DriverID).Scan(&closed)
	if err != nil {
		return err
	}
	if closed {
		return chargeInvalid("Reopen this driver's finalized dispatcher statements before changing home days or comments")
	}
	report, err := r.report(ctx, tx, w, false)
	if err != nil {
		return err
	}
	found := false
	for _, s := range report.Statements {
		for _, row := range s.Rows {
			if row.DriverID == in.DriverID {
				found = true
			}
		}
	}
	if !found {
		return chargeInvalid("This driver has no Gross Board loads in the selected week")
	}
	var before json.RawMessage
	var version int
	err = tx.QueryRow(ctx, `SELECT to_jsonb(i),version FROM dispatcher_pay_inputs i WHERE week_start=$1 AND driver_id=$2`, w, in.DriverID).Scan(&before, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if version != in.Version {
		return ErrDispatcherPayConflict
	}
	var after json.RawMessage
	err = tx.QueryRow(ctx, `INSERT INTO dispatcher_pay_inputs(week_start,driver_id,home_days,note) VALUES($1,$2,$3,$4)
 ON CONFLICT(week_start,driver_id) DO UPDATE SET home_days=excluded.home_days,note=excluded.note,version=dispatcher_pay_inputs.version+1 RETURNING to_jsonb(dispatcher_pay_inputs)`, w, in.DriverID, in.HomeDays, in.Note).Scan(&after)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO dispatcher_pay_history(week_start,actor_id,action,before_value,after_value) VALUES($1,$2,'weekly_input',$3,$4)`, w, actor, before, after)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *DispatcherPayRepository) ChangeStatement(ctx context.Context, week string, in DispatcherStatementChange, actor string, reopen bool) error {
	w, err := dispatcherWeek(week)
	if err != nil {
		return err
	}
	if in.DispatcherID == "" {
		return chargeInvalid("Unassigned loads cannot be finalized")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if reopen && (in.Reason == "" || len([]rune(in.Reason)) > 1000) {
		return chargeInvalid("Provide a reopen reason, up to 1,000 characters")
	}
	// Read committed lets the first reads after a contended accounting lock see
	// the preceding writer's commit. Board/rule/input writers share this lock;
	// effective load amounts are resolved in one SQL statement below.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	var before json.RawMessage
	var closed bool
	var version int
	err = tx.QueryRow(ctx, `SELECT to_jsonb(s),finalized,version FROM dispatcher_pay_statements s WHERE week_start=$1 AND dispatcher_id=$2`, w, in.DispatcherID).Scan(&before, &closed, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if version != in.Version {
		return ErrDispatcherPayConflict
	}
	if reopen {
		if !closed {
			return ErrDispatcherPayConflict
		}
		_, err = tx.Exec(ctx, `UPDATE dispatcher_pay_statements SET finalized=false,version=version+1,actor_id=$3,updated_at=now() WHERE week_start=$1 AND dispatcher_id=$2`, w, in.DispatcherID, actor)
	} else {
		if closed {
			return ErrDispatcherPayConflict
		}
		report, e := r.report(ctx, tx, w, false)
		if e != nil {
			return e
		}
		var selected *DispatcherCommissionStatement
		for i := range report.Statements {
			if report.Statements[i].DispatcherID == in.DispatcherID {
				selected = &report.Statements[i]
				break
			}
		}
		if selected == nil || selected.Revision != in.Revision {
			return ErrDispatcherPayConflict
		}
		for _, row := range selected.Rows {
			if len(row.Issues) > 0 {
				return chargeInvalid("Resolve all Gross Board and commission-rule issues before finalizing")
			}
		}
		raw, e := json.Marshal(selected)
		if e != nil {
			return e
		}
		_, err = tx.Exec(ctx, `INSERT INTO dispatcher_pay_statements(week_start,dispatcher_id,report,actor_id) VALUES($1,$2,$3,$4)
 ON CONFLICT(week_start,dispatcher_id) DO UPDATE SET finalized=true,report=excluded.report,version=dispatcher_pay_statements.version+1,actor_id=excluded.actor_id,updated_at=now()`, w, in.DispatcherID, raw, actor)
	}
	if err != nil {
		return err
	}
	action := "finalize"
	if reopen {
		action = "reopen"
	}
	after, _ := json.Marshal(in)
	_, err = tx.Exec(ctx, `INSERT INTO dispatcher_pay_history(week_start,actor_id,action,before_value,after_value) VALUES($1,$2,$3,$4,$5)`, w, actor, action, before, after)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
