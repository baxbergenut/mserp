package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
)

func allowedChargeAmount(options []string, amount string) bool {
	n, err := chargeCents(amount)
	if err != nil {
		return false
	}
	for _, option := range options {
		v, _ := chargeCents(option)
		if v == n {
			return true
		}
	}
	return false
}

// Driver mutations always acquire driver locks before type locks. Type edits
// need only a type lock, so payroll and management share one lock order.
func lockChargeTypes(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id FROM driver_charge_types ORDER BY id FOR SHARE`)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	return err
}

func (r *DriverChargeRepository) SaveType(ctx context.Context, t ChargeType, actor string) (ChargeType, error) {
	t.Name = strings.TrimSpace(t.Name)
	if len([]rune(t.Name)) < 1 || len([]rune(t.Name)) > 200 || (t.Direction != "charge" && t.Direction != "reimbursement") {
		return t, chargeInvalid("Provide a name and direction")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return t, err
	}
	defer tx.Rollback(ctx)
	oldEligibility := ""
	if t.ID != "" {
		var version int
		var amounts []string
		err = tx.QueryRow(ctx, `SELECT version,eligibility,amounts::text[] FROM driver_charge_types WHERE id=$1 FOR UPDATE`, t.ID).Scan(&version, &oldEligibility, &amounts)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && version != t.Version) {
			return t, ErrChargeConflict
		}
		if err != nil {
			return t, err
		}
		if t.Eligibility == "" {
			t.Eligibility = oldEligibility
		}
		if t.Amounts == nil {
			t.Amounts = amounts
		}
	}
	if t.Eligibility == "" {
		t.Eligibility = "calendar"
	}
	if t.Eligibility != "calendar" && t.Eligibility != "loads" && t.Eligibility != "no_loads" {
		return t, chargeInvalid("Choose calendar weeks, weeks with loads, or weeks without loads")
	}
	if t.Amounts == nil {
		t.Amounts = []string{t.Amount}
	}
	if len(t.Amounts) < 1 || len(t.Amounts) > 50 {
		return t, chargeInvalid("Provide 1–50 amount options")
	}
	seen := map[int64]bool{}
	for i, amount := range t.Amounts {
		n, e := chargeCents(amount)
		if e != nil || n <= 0 || seen[n] {
			return t, chargeInvalid("Amount options must be positive and distinct, with at most two decimals")
		}
		seen[n] = true
		t.Amounts[i] = chargeMoney(n)
	}
	t.Amount = t.Amounts[0]
	if t.ID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO driver_charge_types(name,direction,amount,amounts,eligibility,archived) VALUES($1,$2,$3::numeric,$4::text[]::numeric[],$5,$6) RETURNING id::text,version`, t.Name, t.Direction, t.Amount, t.Amounts, t.Eligibility, t.Archived).Scan(&t.ID, &t.Version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE driver_charge_types SET name=$2,direction=$3,amount=$4::numeric,amounts=$5::text[]::numeric[],eligibility=$6,archived=$7,version=version+1 WHERE id=$1 RETURNING version`, t.ID, t.Name, t.Direction, t.Amount, t.Amounts, t.Eligibility, t.Archived).Scan(&t.Version)
	}
	if err != nil {
		return t, err
	}
	if t.Eligibility != oldEligibility {
		_, err = tx.Exec(ctx, `INSERT INTO driver_charge_type_rules(type_id,week_start,eligibility) VALUES($1,$2::date,$3) ON CONFLICT(type_id,week_start) DO UPDATE SET eligibility=EXCLUDED.eligibility`, t.ID, ChargeCurrentWeek(), t.Eligibility)
		if err != nil {
			return t, err
		}
		// Saved weekly decisions remain explicit; automatic current/future rows
		// must follow the new eligibility rather than an earlier projection.
		_, err = tx.Exec(ctx, `DELETE FROM driver_charge_occurrences o USING driver_charge_schedules s WHERE o.schedule_id=s.id AND s.type_id=$1 AND o.week_start >= $2::date AND NOT o.overridden AND o.confirmed_at IS NULL`, t.ID, ChargeCurrentWeek())
		if err != nil {
			return t, err
		}
	}
	if err = chargeAudit(ctx, tx, "", t.ID, actor, "type_saved", t); err != nil {
		return t, err
	}
	return t, tx.Commit(ctx)
}
