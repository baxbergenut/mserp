package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type TollFlagReviewInput struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Version  string `json:"version"`
	Reviewed bool   `json:"reviewed"`
}

// Fingerprints exclude routine load sync timestamps. Changed amounts, dates,
// attribution or surrounding load evidence require a new explicit review.
func populateTollReviews(ctx context.Context, tx pgx.Tx, values []Toll) (map[string]string, error) {
	indices := map[string]int{}
	ids := []string{}
	evidence := map[string]string{}
	for i, v := range values {
		if v.Flag != nil && v.Flag.Status == "review" {
			ids = append(ids, v.ID)
			indices[v.ID] = i
		}
	}
	if len(ids) == 0 {
		return evidence, nil
	}
	rows, err := tx.Query(ctx, `SELECT t.id::text,jsonb_build_array(t.id,t.truck_id,tr.unit_number,t.posting_date,t.exit_date,t.exit_time,t.entry_date,t.amount,t.equipment_unit,t.prepass_tag_id,t.transponder_or_plate,t.customer_id,t.prepass_environment)::text,coalesce(e.id::text,''),coalesce(e.revision,''),coalesce(e.reviewed,false),coalesce(to_char(e.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),''),coalesce(u.username,'') FROM tolls t LEFT JOIN trucks tr ON tr.id=t.truck_id LEFT JOIN LATERAL (SELECT * FROM toll_flag_review_events WHERE toll_id=t.id ORDER BY sequence DESC LIMIT 1) e ON true LEFT JOIN app_users u ON u.id=e.actor_id WHERE t.id=ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, source, version, revision, at, by string
		var reviewed bool
		if err = rows.Scan(&id, &source, &version, &revision, &reviewed, &at, &by); err != nil {
			return nil, err
		}
		v := &values[indices[id]]
		flag := *v.Flag
		flag.LoadsSyncedAt = nil
		raw, err := json.Marshal(struct {
			Source json.RawMessage `json:"source"`
			Flag   TransactionFlag `json:"flag"`
		}{json.RawMessage(source), flag})
		if err != nil {
			return nil, err
		}
		v.ReviewRevision = fmt.Sprintf("%x", sha256.Sum256(raw))
		v.ReviewVersion = version
		v.Reviewed = reviewed && revision == v.ReviewRevision
		if v.Reviewed {
			v.ReviewedAt = at
			v.ReviewedBy = by
		}
		evidence[id] = string(raw)
	}
	return evidence, rows.Err()
}

func (r *TollRepository) ReviewTollFlag(ctx context.Context, tollID string, in TollFlagReviewInput, actor string) error {
	var request pgtype.UUID
	raw, err := hex.DecodeString(in.Revision)
	if request.Scan(in.ID) != nil || !request.Valid || err != nil || len(raw) != 32 {
		return &TollLinkError{"Refresh the flagged transaction before reviewing it."}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// The toll row serializes reviews and links without changing financial data.
	toll, err := scanToll(tx.QueryRow(ctx, selectTollsSQL+` WHERE t.id=$1 FOR UPDATE OF t`, tollID))
	if errors.Is(err, pgx.ErrNoRows) {
		return &TollLinkError{"Transaction no longer exists."}
	}
	if err != nil {
		return err
	}
	var identical bool
	err = tx.QueryRow(ctx, `SELECT toll_id=$2::uuid AND revision=$3 AND previous_version=$4 AND reviewed=$5 FROM toll_flag_review_events WHERE id=$1`, in.ID, tollID, in.Revision, in.Version, in.Reviewed).Scan(&identical)
	if err == nil {
		if identical {
			return nil
		}
		return &TollLinkError{"This review request was already used. Refresh the transactions."}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	flags, err := transactionFlagsTx(ctx, tx, "toll", []string{tollID}, time.Now().UTC())
	if err != nil {
		return err
	}
	toll.Flag = flags[tollID]
	if toll.Flag == nil || toll.Flag.Status != "review" {
		return &TollLinkError{"This transaction is no longer flagged. Refresh the list."}
	}
	values := []Toll{toll}
	evidence, err := populateTollReviews(ctx, tx, values)
	if err != nil {
		return err
	}
	toll = values[0]
	if toll.ReviewRevision != in.Revision || toll.ReviewVersion != in.Version {
		return &TollLinkError{"The transaction or its review changed. Refresh and review it again."}
	}
	if !in.Reviewed && !toll.Reviewed {
		return &TollLinkError{"This flag is already open for review."}
	}
	_, err = tx.Exec(ctx, `INSERT INTO toll_flag_review_events(id,toll_id,revision,previous_version,reviewed,evidence,actor_id) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7)`, in.ID, tollID, in.Revision, in.Version, in.Reviewed, evidence[tollID], actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
