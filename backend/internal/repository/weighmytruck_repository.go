package repository

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/weighmytruck"
)

var ErrWMTConflict = errors.New("WeighMyTruck record changed or needs verification. Refresh the list.")

type WMTValidationError struct{ Message string }

func (e *WMTValidationError) Error() string { return e.Message }

type WMTProvider interface {
	Configured() bool
	Change(context.Context, bool, weighmytruck.Driver) error
}
type WeighMyTruckRepository struct {
	pool     *pgxpool.Pool
	provider WMTProvider
}

func NewWeighMyTruckRepository(pool *pgxpool.Pool, p WMTProvider) *WeighMyTruckRepository {
	return &WeighMyTruckRepository{pool: pool, provider: p}
}

type WMTEntry struct {
	ID             string     `json:"id"`
	DriverID       string     `json:"driverId"`
	Name           string     `json:"name"`
	Email          string     `json:"email"`
	Phone          string     `json:"phone"`
	DriverCode     string     `json:"driverCode"`
	DriverStatus   string     `json:"driverStatus"`
	Enrolled       bool       `json:"enrolled"`
	State          string     `json:"state"`
	Version        int        `json:"version"`
	Warning        string     `json:"warning"`
	AddBlockReason string     `json:"addBlockReason"`
	CanVerify      bool       `json:"canVerify"`
	UpdatedAt      *time.Time `json:"updatedAt"`
}
type WMTList struct {
	Items       []WMTEntry `json:"items"`
	Configured  bool       `json:"configured"`
	Initialized bool       `json:"initialized"`
}
type WMTChange struct {
	DriverID     string `json:"driverId"`
	MembershipID string `json:"membershipId"`
	Version      int    `json:"version"`
	Add          bool   `json:"add"`
}
type WMTVerification struct {
	Version  int    `json:"version"`
	Enrolled bool   `json:"enrolled"`
	Reason   string `json:"reason"`
}

func wmtDriver(name, email, phone string) (weighmytruck.Driver, error) {
	parts := strings.Fields(name)
	if len(parts) < 2 {
		return weighmytruck.Driver{}, &WMTValidationError{"Add a first and last name to the driver profile."}
	}
	email = strings.ToLower(strings.TrimSpace(email))
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email {
		return weighmytruck.Driver{}, &WMTValidationError{"Add a valid email to the driver profile."}
	}
	if len(phone) != 10 || strings.Trim(phone, "0123456789") != "" {
		return weighmytruck.Driver{}, &WMTValidationError{"Add a valid phone number to the driver profile."}
	}
	return weighmytruck.Driver{FirstName: parts[0], LastName: strings.Join(parts[1:], " "), Email: email, Phone: phone}, nil
}

func (r *WeighMyTruckRepository) List(ctx context.Context) (WMTList, error) {
	result := WMTList{Items: []WMTEntry{}, Configured: r.provider != nil && r.provider.Configured()}
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_imports)`).Scan(&result.Initialized)
	if err != nil {
		return result, err
	}
	rows, err := r.pool.Query(ctx, `SELECT COALESCE(m.id::text,''),COALESCE(d.id::text,''),
 COALESCE(d.full_name,m.first_name||' '||m.last_name),COALESCE(m.email,d.email,''),COALESCE(m.phone,d.phone,''),COALESCE(m.driver_code,''),
 COALESCE(d.status,'unlinked'),COALESCE(m.enrolled,false),COALESCE(m.state,'confirmed'),COALESCE(m.version,0),COALESCE(m.last_error,''),m.updated_at,
 COALESCE(d.email,''),COALESCE(d.phone,''),
 COALESCE(m.state='review' OR (m.state='pending' AND m.updated_at<now()-interval '2 minutes'),false)
 FROM drivers d FULL JOIN weighmytruck_memberships m ON m.driver_id=d.id
 ORDER BY COALESCE(m.enrolled,false) DESC,lower(COALESCE(d.full_name,m.first_name||' '||m.last_name))`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var e WMTEntry
		var currentEmail, currentPhone, lastError string
		if err := rows.Scan(&e.ID, &e.DriverID, &e.Name, &e.Email, &e.Phone, &e.DriverCode, &e.DriverStatus, &e.Enrolled, &e.State, &e.Version, &lastError, &e.UpdatedAt, &currentEmail, &currentPhone, &e.CanVerify); err != nil {
			return result, err
		}
		if e.DriverStatus == "terminated" && (e.Enrolled || e.State != "confirmed") {
			e.Warning = "Terminated driver still has, or may have, WeighMyTruck access."
		}
		if e.DriverID == "" {
			e.Warning = "Not linked to an MSERP driver."
			e.AddBlockReason = "Link this account to a driver before adding it again."
		}
		if e.State != "confirmed" || lastError != "" {
			e.Warning = strings.TrimSpace(e.Warning + " " + lastError)
			if lastError == "" {
				e.Warning = strings.TrimSpace(e.Warning + " Membership change in progress; verify if it does not complete.")
			}
		}
		if e.DriverID != "" {
			if e.DriverStatus == "terminated" {
				e.AddBlockReason = "Terminated drivers cannot be added."
			} else if _, err := wmtDriver(e.Name, currentEmail, currentPhone); err != nil {
				e.AddBlockReason = err.Error()
			}
			if e.Enrolled && !strings.EqualFold(e.Email, strings.TrimSpace(currentEmail)) {
				e.Warning = strings.TrimSpace(e.Warning + " Profile email differs; removal uses the enrolled email.")
			}
		}
		result.Items = append(result.Items, e)
	}
	return result, rows.Err()
}

// Reserve durably before calling the provider. A lost reply or process crash
// leaves a visible pending/review record instead of silently replaying a POST.
func (r *WeighMyTruckRepository) reserve(ctx context.Context, in WMTChange, actor string) (string, int, weighmytruck.Driver, error) {
	var d weighmytruck.Driver
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", 0, d, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(730073)`); err != nil {
		return "", 0, d, err
	}
	var initialized bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_imports)`).Scan(&initialized); err != nil {
		return "", 0, d, err
	}
	if !initialized {
		return "", 0, d, &WMTValidationError{"Import the existing WeighMyTruck roster before making changes."}
	}
	id := in.MembershipID
	if in.Add {
		var name, email, phone, status string
		err = tx.QueryRow(ctx, `SELECT full_name,COALESCE(email,''),COALESCE(phone,''),status FROM drivers WHERE id=$1 FOR UPDATE`, in.DriverID).Scan(&name, &email, &phone, &status)
		if err != nil {
			return "", 0, d, err
		}
		if status == "terminated" {
			return "", 0, d, &WMTValidationError{"Terminated drivers cannot be added."}
		}
		d, err = wmtDriver(name, email, phone)
		if err != nil {
			return "", 0, d, err
		}
		var duplicate bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drivers WHERE id<>$1 AND lower(btrim(email))=$2)`, in.DriverID, d.Email).Scan(&duplicate)
		if err != nil {
			return "", 0, d, err
		}
		if duplicate {
			return "", 0, d, &WMTValidationError{"This email belongs to multiple MSERP drivers. Correct their profiles first."}
		}
		if id == "" {
			if in.Version != 0 {
				return "", 0, d, ErrWMTConflict
			}
			err = tx.QueryRow(ctx, `INSERT INTO weighmytruck_memberships(driver_id,email,first_name,last_name,phone,version) VALUES($1,$2,$3,$4,$5,0) ON CONFLICT DO NOTHING RETURNING id::text`, in.DriverID, d.Email, d.FirstName, d.LastName, d.Phone).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return "", 0, d, ErrWMTConflict
			}
			if err != nil {
				return "", 0, d, err
			}
		}
	}
	var linked, email, first, last, phone, state, code string
	var version int
	var enrolled bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(driver_id::text,''),email,first_name,last_name,phone,state,version,enrolled,driver_code FROM weighmytruck_memberships WHERE id=$1 FOR UPDATE`, id).Scan(&linked, &email, &first, &last, &phone, &state, &version, &enrolled, &code)
	if err != nil {
		return "", 0, d, err
	}
	if state != "confirmed" || version != in.Version || enrolled == in.Add || (in.Add && linked != in.DriverID) {
		return "", 0, d, ErrWMTConflict
	}
	if !in.Add {
		d = weighmytruck.Driver{Email: email, FirstName: first, LastName: last, Phone: phone}
	}
	d.UniqueID = code
	// A removed driver's changed email is safe only if no other tracked account owns it.
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_memberships WHERE email=$1 AND id<>$2)`, d.Email, id).Scan(&exists); err != nil {
		return "", 0, d, err
	}
	if exists {
		return "", 0, d, &WMTValidationError{"This email is already tracked by another WeighMyTruck account."}
	}
	_, err = tx.Exec(ctx, `UPDATE weighmytruck_memberships SET email=$2,first_name=$3,last_name=$4,phone=$5,state='pending',last_error='',version=version+1,updated_at=now() WHERE id=$1`, id, d.Email, d.FirstName, d.LastName, d.Phone)
	if err != nil {
		return "", 0, d, err
	}
	action := "remove_requested"
	if in.Add {
		action = "add_requested"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weighmytruck_events(membership_id,actor_id,action) VALUES($1,$2,$3)`, id, actor, action); err != nil {
		return "", 0, d, err
	}
	return id, version + 1, d, tx.Commit(ctx)
}

func (r *WeighMyTruckRepository) Change(ctx context.Context, in WMTChange, actor string) error {
	if r.provider == nil || !r.provider.Configured() {
		return &WMTValidationError{"WeighMyTruck is not configured on the server."}
	}
	id, version, d, err := r.reserve(ctx, in, actor)
	if err != nil {
		return err
	}
	// Finish an accepted request even if the browser disconnects. No retries.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 55*time.Second)
	callErr := r.provider.Change(callCtx, in.Add, d)
	cancel()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finishCancel()
	tx, err := r.pool.Begin(finishCtx)
	if err != nil {
		return err
	}
	defer tx.Rollback(finishCtx)
	state, message, action := "confirmed", "", "remove_succeeded"
	if in.Add {
		action = "add_succeeded"
	}
	if callErr != nil {
		state = "review"
		message = "WeighMyTruck result is uncertain. Verify membership on the fleet website."
		action = "change_uncertain"
		var e *weighmytruck.Error
		if errors.As(callErr, &e) {
			message = e.Message
			if !e.Uncertain {
				state = "confirmed"
				action = "change_rejected"
			}
		}
	}
	tag, err := tx.Exec(finishCtx, `UPDATE weighmytruck_memberships SET enrolled=CASE WHEN $3 THEN $4 ELSE enrolled END,state=$5,last_error=$6,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 AND state='pending'`, id, version, callErr == nil, in.Add, state, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWMTConflict
	}
	if _, err = tx.Exec(finishCtx, `INSERT INTO weighmytruck_events(membership_id,actor_id,action,detail) VALUES($1,$2,$3,$4)`, id, actor, action, message); err != nil {
		return err
	}
	if err = tx.Commit(finishCtx); err != nil {
		return err
	}
	return callErr
}

// Verification is an explicit observation of the website, never an API retry.
func (r *WeighMyTruckRepository) Verify(ctx context.Context, id string, in WMTVerification, actor string) error {
	if strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 1000 {
		return &WMTValidationError{"Describe what you verified on the WeighMyTruck website."}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE weighmytruck_memberships SET enrolled=$3,state='confirmed',version=version+1,last_error='',updated_at=now() WHERE id=$1 AND version=$2 AND (state<>'pending' OR updated_at<now()-interval '2 minutes')`, id, in.Version, in.Enrolled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWMTConflict
	}
	action := "verified_removed"
	if in.Enrolled {
		action = "verified_enrolled"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weighmytruck_events(membership_id,actor_id,action,detail) VALUES($1,$2,$3,$4)`, id, actor, action, strings.TrimSpace(in.Reason)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
