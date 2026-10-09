package repository

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"net/mail"
	"strings"
)

type WMTImportResult struct {
	Total, Linked, Unlinked int
	AlreadyImported         bool
}

// ImportWeighMyTruck initializes an empty tracker from the fleet website's CSV.
// Imports never call the provider or overwrite membership changes.
func (r *WeighMyTruckRepository) ImportWeighMyTruck(ctx context.Context, source io.Reader) (WMTImportResult, error) {
	var result WMTImportResult
	body, err := io.ReadAll(io.LimitReader(source, 2<<20))
	if err != nil {
		return result, err
	}
	if len(body) >= 2<<20 {
		return result, fmt.Errorf("roster exceeds 2 MB")
	}
	digest := sha256.Sum256(body)
	key := hex.EncodeToString(digest[:])
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\ufeff")))
	records, err := reader.ReadAll()
	if err != nil {
		return result, fmt.Errorf("invalid roster CSV")
	}
	if len(records) < 1 {
		return result, fmt.Errorf("missing CSV header")
	}
	want := []string{"First Name", "Last Name", "Email", "Phone", "Driver Code"}
	if len(records[0]) < 5 {
		return result, fmt.Errorf("invalid roster header")
	}
	for i, h := range want {
		if records[0][i] != h {
			return result, fmt.Errorf("expected roster column %s", h)
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(730073)`); err != nil {
		return result, err
	}
	var prior bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_imports WHERE digest=$1)`, key).Scan(&prior); err != nil {
		return result, err
	}
	if prior {
		result.AlreadyImported = true
		return result, nil
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_memberships) OR EXISTS(SELECT 1 FROM weighmytruck_imports)`).Scan(&prior); err != nil {
		return result, err
	}
	if prior {
		return result, fmt.Errorf("tracker already initialized; reconcile explicitly instead of overwriting")
	}
	seen := map[string]bool{}
	for i, row := range records[1:] {
		for j := range row {
			row[j] = strings.TrimSpace(row[j])
		}
		email := strings.ToLower(row[2])
		a, e := mail.ParseAddress(email)
		if e != nil || a.Address != email || row[0] == "" || row[1] == "" || seen[email] {
			return result, fmt.Errorf("invalid or duplicate roster row %d", i+2)
		}
		seen[email] = true
		phone := strings.Map(func(c rune) rune {
			if c >= '0' && c <= '9' {
				return c
			}
			return -1
		}, row[3])
		if len(phone) == 11 && phone[0] == '1' {
			phone = phone[1:]
		}
		var candidates []string
		rows, e := tx.Query(ctx, `SELECT id::text FROM drivers WHERE lower(btrim(email))=$1 OR (phone=$2 AND $2<>'')`, email, phone)
		if e != nil {
			return result, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			candidates = append(candidates, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		var driverID any
		if len(candidates) == 1 {
			var used bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighmytruck_memberships WHERE driver_id=$1)`, candidates[0]).Scan(&used); err != nil {
				return result, err
			}
			if !used {
				driverID = candidates[0]
				result.Linked++
			} else {
				result.Unlinked++
			}
		} else {
			result.Unlinked++
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO weighmytruck_memberships(driver_id,email,first_name,last_name,phone,driver_code,enrolled) VALUES($1,$2,$3,$4,$5,$6,true) RETURNING id::text`, driverID, email, row[0], row[1], phone, row[4]).Scan(&id)
		if err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO weighmytruck_events(membership_id,actor_id,action,detail) VALUES($1,'operator','imported',$2)`, id, "Fleet website CSV SHA-256 "+key); err != nil {
			return result, err
		}
		result.Total++
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weighmytruck_imports(digest,row_count) VALUES($1,$2)`, key, result.Total); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *WeighMyTruckRepository) Link(ctx context.Context, id, driverID string, version int, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(730073)`); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drivers WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM weighmytruck_memberships WHERE driver_id=$1)`, driverID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrWMTConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE weighmytruck_memberships SET driver_id=$2,version=version+1,updated_at=now() WHERE id=$1 AND version=$3 AND driver_id IS NULL AND state='confirmed'`, id, driverID, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWMTConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO weighmytruck_events(membership_id,actor_id,action,detail) VALUES($1,$2,'linked',$3)`, id, actor, driverID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
