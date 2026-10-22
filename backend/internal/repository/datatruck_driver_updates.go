package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
	"mserp/internal/phone"
)

func dataTruckCreatedProfile(payload map[string]any) map[string]any {
	profile := map[string]any{}
	for _, key := range []string{"first_name", "last_name", "email", "contact_number", "brith_date", "driver_license", "driver_license_state", "driver_license_expiration"} {
		profile[key] = payload[key]
	}
	return profile
}

// A success-only creation receipt is resolved by read-only discovery. It never
// retries creation or adopts an account from an unsuccessful creation attempt.
func (r *DataTruckRepository) resolveCreatedDriverIdentity(ctx context.Context, conn *pgxpool.Conn) error {
	var driver string
	var raw []byte
	err := conn.QueryRow(ctx, `SELECT entity_id::text,payload FROM datatruck_enrollments WHERE entity_kind='drivers' AND state='done' AND provider_id IS NULL AND sent_at IS NOT NULL AND payload IS NOT NULL AND environment=$1 AND provider_carrier_id=$2 AND updated_at<now()-interval '1 minute' ORDER BY updated_at LIMIT 1`, r.client.Environment(), r.mcID).Scan(&driver, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	contact, _ := payload["contact_number"].(string)
	rows, err := r.client.FleetList(ctx, "drivers", []map[string]string{{"column": "contact_number", "value": contact, "contains": "is"}, {"column": "mc_number", "value": strconv.Itoa(r.mcID), "contains": "is"}})
	if err != nil || len(rows) != 1 {
		_, e := conn.Exec(ctx, `UPDATE datatruck_enrollments SET updated_at=now(),last_error='Account created. Its DataTruck ID still needs verification.' WHERE entity_kind='drivers' AND entity_id=$1 AND provider_id IS NULL`, driver)
		return e
	}
	account, _ := rows[0]["account"].(map[string]any)
	for _, key := range []string{"first_name", "last_name", "email"} {
		if payload[key] != nil && !strings.EqualFold(fmt.Sprint(account[key]), fmt.Sprint(payload[key])) {
			return nil
		}
	}
	provider, err := strconv.ParseInt(fmt.Sprint(rows[0]["id"]), 10, 64)
	if err != nil || provider <= 0 {
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var local []byte
	if err = tx.QueryRow(ctx, `SELECT datatruck_driver_profile(d) FROM drivers d WHERE id=$1 FOR SHARE`, driver).Scan(&local); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	evidence, _ := json.Marshal(map[string]any{"basis": "successful creation receipt and unique matching provider identity", "provider": rows[0]})
	_, err = tx.Exec(ctx, `UPDATE datatruck_enrollments SET provider_id=$2,identity_evidence=$3,version=version+1,last_error='',updated_at=now() WHERE entity_kind='drivers' AND entity_id=$1 AND provider_id IS NULL AND state='done'`, driver, provider, evidence)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO datatruck_driver_updates(driver_id,provider_id,environment,carrier_id,source_snapshot) SELECT entity_id,provider_id,environment,provider_carrier_id,$2::jsonb FROM datatruck_enrollments WHERE entity_kind='drivers' AND entity_id=$1 AND profile_baseline IS DISTINCT FROM $2::jsonb`, driver, local)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// The mandatory PUT envelope preserves DataTruck's dispatch/lifecycle fields.
// Only the profile allowlist is owned here. Assignment objects are never sent.
func dataTruckUpdatePayload(source, baseline, remote map[string]any, carrier int) (map[string]any, error) {
	invalid := func(message string) (map[string]any, error) { return nil, &DriverServiceError{message} }
	text := func(value any) string { s, _ := value.(string); return strings.TrimSpace(s) }
	if text(source["brith_date"]) == "" {
		return invalid("Record a verified date of birth before syncing this driver to DataTruck.")
	}
	if remote["team_driver"] != nil {
		return invalid("Verify the main-driver role in DataTruck before syncing a team driver.")
	}
	account, ok := remote["account"].(map[string]any)
	if !ok {
		return invalid("DataTruck did not return a complete account. Review the driver there.")
	}
	value := func(key string) string {
		if s := text(source[key]); s != "" && !reflect.DeepEqual(source[key], baseline[key]) {
			return s
		}
		return text(account[key])
	}
	first, last, email := value("first_name"), value("last_name"), value("email")
	contactValue := remote["contact_number"]
	if !reflect.DeepEqual(source["contact_number"], baseline["contact_number"]) {
		contactValue = source["contact_number"]
	}
	contact, err := phone.Normalize(text(contactValue))
	if err != nil || contact == "" || first == "" || last == "" || email == "" {
		return invalid("Record the driver's name, email and valid phone before syncing to DataTruck.")
	}
	if text(remote["driver_type"]) == "" || text(remote["employee_status"]) == "" || text(remote["status"]) == "" {
		return invalid("DataTruck did not return the required driver status. Review the profile there.")
	}
	payload := map[string]any{"account": map[string]any{"first_name": first, "last_name": last, "email": email}, "contact_number": "+1" + contact,
		"brith_date": source["brith_date"], "driver_type": remote["driver_type"], "new_employee_status": remote["employee_status"], "status": remote["status"], "is_main": true, "mc_number": carrier}
	for _, key := range []string{"driver_license", "driver_license_state", "driver_license_expiration"} {
		if !reflect.DeepEqual(source[key], baseline[key]) {
			payload[key] = source[key]
		}
	}
	return payload, nil
}

func dataTruckProfileReadback(payload, remote map[string]any) bool {
	account, _ := remote["account"].(map[string]any)
	wanted, _ := payload["account"].(map[string]any)
	for _, key := range []string{"first_name", "last_name", "email"} {
		if account[key] != wanted[key] {
			return false
		}
	}
	a, _ := remote["contact_number"].(string)
	b, _ := payload["contact_number"].(string)
	return phone.Imported(a) != nil && phone.Imported(b) != nil && *phone.Imported(a) == *phone.Imported(b)
}

// Each PUT has a committed request and one attempt. Unknown outcomes block later
// updates for that driver; a newer edit can replace a definite rejection only.
func (r *DataTruckRepository) ProcessDriverUpdates(ctx context.Context) error {
	if !r.Configured() {
		return nil
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(736281949)`); err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(c, `SELECT pg_advisory_unlock(736281949)`); e != nil {
			_ = conn.Conn().Close(c)
		}
	}()
	if err = r.resolveCreatedDriverIdentity(ctx, conn); err != nil {
		return err
	}
	if _, err = conn.Exec(ctx, `UPDATE datatruck_driver_updates SET state=CASE WHEN sent_at IS NULL THEN 'review' ELSE 'unconfirmed' END,last_error='Update was interrupted. Check DataTruck before resending.',updated_at=now() WHERE state='running' AND updated_at<now()-interval '10 minutes'`); err != nil {
		return err
	}
	var id, provider int64
	var driver, environment string
	var carrier int
	var raw, base []byte
	err = conn.QueryRow(ctx, `UPDATE datatruck_driver_updates u SET state='running',updated_at=now()
 WHERE id=(SELECT q.id FROM datatruck_driver_updates q WHERE state='queued'
 AND NOT EXISTS(SELECT 1 FROM datatruck_driver_updates b WHERE b.driver_id=q.driver_id AND b.state IN ('running','unconfirmed')) ORDER BY q.id LIMIT 1)
 RETURNING id,driver_id::text,provider_id,environment,carrier_id,source_snapshot`).Scan(&id, &driver, &provider, &environment, &carrier, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	finish := func(state, message string, receipt json.RawMessage) error {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, e := conn.Exec(c, `UPDATE datatruck_driver_updates SET state=$2,last_error=$3,receipt=$4,updated_at=now() WHERE id=$1 AND state='running'`, id, state, message, receipt)
		return e
	}
	if environment != r.client.Environment() || carrier != r.mcID {
		return finish("review", "DataTruck tenant or carrier changed. Review the saved account link.", nil)
	}
	err = conn.QueryRow(ctx, `SELECT e.profile_baseline FROM datatruck_enrollments e JOIN drivers d ON d.id=e.entity_id WHERE e.entity_kind='drivers' AND e.entity_id=$1 AND e.provider_id=$2 AND e.environment=$3 AND e.provider_carrier_id=$4 AND e.state='done'`, driver, provider, environment, carrier).Scan(&base)
	if errors.Is(err, pgx.ErrNoRows) {
		return finish("canceled", "The original DataTruck link is no longer available.", nil)
	}
	if err != nil {
		return err
	}
	var source, baseline map[string]any
	if json.Unmarshal(raw, &source) != nil || json.Unmarshal(base, &baseline) != nil {
		return finish("review", "The saved profile snapshot needs review.", nil)
	}
	if reflect.DeepEqual(source, baseline) {
		return finish("superseded", "", nil)
	}
	filters := []map[string]string{{"column": "id", "value": strconv.FormatInt(provider, 10), "contains": "is"}, {"column": "mc_number", "value": strconv.Itoa(carrier), "contains": "is"}}
	current, e := r.client.FleetList(ctx, "drivers", filters)
	if e != nil || len(current) != 1 {
		return finish("review", "Could not verify the linked DataTruck account before updating.", nil)
	}
	if fmt.Sprint(current[0]["id"]) != strconv.FormatInt(provider, 10) {
		return finish("review", "DataTruck returned a different account. Review the link.", nil)
	}
	payload, e := dataTruckUpdatePayload(source, baseline, current[0], carrier)
	if e != nil {
		return finish("review", e.Error(), nil)
	}
	body, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	if _, e = conn.Exec(ctx, `UPDATE datatruck_driver_updates SET request_payload=$2,sent_at=now(),updated_at=now() WHERE id=$1 AND state='running'`, id, body); e != nil {
		return e
	}
	receipt, e := r.client.UpdateDriver(ctx, provider, body)
	if e != nil {
		state := "unconfirmed"
		var upstream *datatruck.FleetError
		if errors.As(e, &upstream) && !upstream.Uncertain {
			state = "review"
		}
		return finish(state, "DataTruck did not confirm the profile update. Review the saved request before resending.", receipt)
	}
	current, e = r.client.FleetList(ctx, "drivers", filters)
	if e != nil || len(current) != 1 || !dataTruckProfileReadback(payload, current[0]) {
		return finish("unconfirmed", "DataTruck accepted the update but its profile could not be verified. Review before resending.", receipt)
	}
	// Commit the accepted baseline and receipt together. A concurrent local edit
	// retains its own queued snapshot and is processed against this baseline next.
	tx, e := conn.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE datatruck_enrollments SET profile_baseline=$2 WHERE entity_kind='drivers' AND entity_id=$1 AND provider_id=$3 AND environment=$4`, driver, raw, provider, environment); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE datatruck_driver_updates SET state='accepted',receipt=$2,last_error='',updated_at=now() WHERE id=$1 AND state='running'`, id, receipt); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
