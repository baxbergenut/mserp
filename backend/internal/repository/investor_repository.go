package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const CompanyOwnerID = "00000000-0000-0000-0000-000000000001"

var ErrInvestorConflict = errors.New("company owners cannot be edited and a linked driver cannot be changed; edit the driver profile for contact details")
var ErrInactiveOwner = errors.New("select an active investor as the new owner")

type InvestorTruck struct {
	ID         string `json:"id"`
	UnitNumber string `json:"unitNumber"`
}
type Investor struct {
	ID        string          `json:"id"`
	FullName  string          `json:"fullName"`
	DriverID  *string         `json:"driverId"`
	IsCompany bool            `json:"isCompany"`
	Email     *string         `json:"email"`
	Phone     *string         `json:"phone"`
	Notes     *string         `json:"notes"`
	Active    bool            `json:"active"`
	Trucks    []InvestorTruck `json:"trucks"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
type InvestorInput struct {
	FullName string
	DriverID *string
	Email    *string
	Phone    *string
	Notes    *string
	Active   bool
}

const selectInvestorsSQL = `SELECT i.id,coalesce(d.full_name,i.full_name),i.driver_id,i.is_company,
 CASE WHEN d.id IS NULL THEN i.email ELSE d.email END,
 CASE WHEN d.id IS NULL THEN i.phone ELSE d.phone END,i.notes,i.active,
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'unitNumber',t.unit_number) ORDER BY t.unit_number)
 FROM trucks t WHERE t.owner_id=i.id),'[]'::jsonb),i.created_at,i.updated_at
 FROM investors i LEFT JOIN drivers d ON d.id=i.driver_id`

func scanInvestor(row rowScanner) (Investor, error) {
	var v Investor
	err := row.Scan(&v.ID, &v.FullName, &v.DriverID, &v.IsCompany, &v.Email, &v.Phone, &v.Notes, &v.Active, &v.Trucks, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (r *FleetRepository) ListInvestors(ctx context.Context, search string) ([]Investor, error) {
	rows, err := r.pool.Query(ctx, selectInvestorsSQL+` WHERE $1='' OR concat_ws(' ',coalesce(d.full_name,i.full_name),coalesce(d.email,i.email),coalesce(d.phone,i.phone)) ILIKE '%'||$1||'%'
 OR EXISTS(SELECT 1 FROM trucks t WHERE t.owner_id=i.id AND t.unit_number ILIKE '%'||$1||'%')
 ORDER BY i.is_company DESC,coalesce(d.full_name,i.full_name),i.id`, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Investor, 0)
	for rows.Next() {
		v, e := scanInvestor(rows)
		if e != nil {
			return nil, e
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (r *FleetRepository) GetInvestor(ctx context.Context, id string) (Investor, error) {
	v, e := scanInvestor(r.pool.QueryRow(ctx, selectInvestorsSQL+` WHERE i.id=$1`, id))
	return v, mapNotFound(e)
}
func (r *FleetRepository) SaveInvestor(ctx context.Context, id string, input InvestorInput) (Investor, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Investor{}, err
	}
	defer tx.Rollback(ctx)
	if input.DriverID != nil {
		err = tx.QueryRow(ctx, `SELECT full_name,email,phone FROM drivers WHERE id=$1 FOR SHARE`, input.DriverID).Scan(&input.FullName, &input.Email, &input.Phone)
		if err != nil {
			return Investor{}, mapNotFound(err)
		}
	} else {
		input.FullName = formatPersonName(input.FullName)
	}
	if id != "" {
		var company bool
		var driver *string
		if err = tx.QueryRow(ctx, `SELECT is_company,driver_id FROM investors WHERE id=$1 FOR UPDATE`, id).Scan(&company, &driver); err != nil {
			return Investor{}, mapNotFound(err)
		}
		if company || (driver == nil) != (input.DriverID == nil) || (driver != nil && *driver != *input.DriverID) {
			return Investor{}, ErrInvestorConflict
		}
	}

	if id == "" {
		err = tx.QueryRow(ctx, `INSERT INTO investors(full_name,driver_id,email,phone,notes,active) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, input.FullName, input.DriverID, input.Email, input.Phone, input.Notes, input.Active).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE investors SET full_name=$2,email=$3,phone=$4,notes=$5,active=$6,updated_at=now() WHERE id=$1`, id, input.FullName, input.Email, input.Phone, input.Notes, input.Active)
	}
	if err != nil {
		return Investor{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Investor{}, err
	}
	return r.GetInvestor(ctx, id)
}

// A missing owner preserves existing ownership for older API clients.
func setTruckOwner(ctx context.Context, tx pgx.Tx, id string, ownerID *string) error {
	if ownerID == nil {
		return nil
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT active FROM investors WHERE id=$1 FOR SHARE`, *ownerID).Scan(&active); err != nil {
		return mapNotFound(err)
	}
	if !active {
		var unchanged bool
		if err := tx.QueryRow(ctx, `SELECT owner_id=$2 FROM trucks WHERE id=$1`, id, *ownerID).Scan(&unchanged); err != nil {
			return err
		}
		if !unchanged {
			return ErrInactiveOwner
		}
	}
	_, err := tx.Exec(ctx, `UPDATE trucks SET owner_id=$2 WHERE id=$1 AND owner_id IS DISTINCT FROM $2::uuid`, id, *ownerID)
	return err
}
