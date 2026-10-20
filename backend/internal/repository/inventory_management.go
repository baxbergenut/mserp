package repository

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func inventoryUnavailable(d InventoryFields) bool {
	return !d.Active || slices.Contains([]string{"lost", "retired"}, d.Condition) || slices.Contains([]string{"sold", "total_loss", "lost", "retired"}, d.Status)
}

func validateInventoryDetails(d InventoryFields) error {
	for _, value := range []string{d.ModelNumber, d.Color, d.CaseColor} {
		if value != strings.TrimSpace(value) || len(value) > 50 {
			return inventoryInvalid("Device details must be trimmed and 50 characters or fewer")
		}
	}
	if d.StorageGB != "" {
		n, err := strconv.Atoi(d.StorageGB)
		if err != nil || n < 1 || n > 65536 || strconv.Itoa(n) != d.StorageGB {
			return inventoryInvalid("Storage capacity must be a whole number of GB between 1 and 65536")
		}
	}
	return nil
}

type InventoryChange struct {
	Collection string          `json:"collection"`
	Record     InventoryRecord `json:"record"`
}
type InventoryOperation struct {
	Verify        bool                `json:"verify,omitempty"`
	RequestID     string              `json:"requestId"`
	Reason        string              `json:"reason"`
	EffectiveDate string              `json:"effectiveDate"`
	Changes       []InventoryChange   `json:"changes"`
	ExpectedLinks map[string][]string `json:"expectedLinks"`
}

// A reviewed set of final records is committed as one operation. Relationships
// are released only inside this transaction, permitting swaps without losing
// original audit snapshots or exposing an intermediate state to other readers.
func (r *InventoryRepository) ApplyOperation(ctx context.Context, input InventoryOperation, actor InventoryActor) ([]InventoryChange, error) {
	if !inventoryUUID.MatchString(input.RequestID) || len(input.Changes) < 1 || len(input.Changes) > 25 {
		return nil, inventoryInvalid("Choose 1–25 records and a unique request ID")
	}
	if strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 1000 {
		return nil, inventoryInvalid("Enter a reason of 1000 characters or fewer")
	}
	date, err := time.Parse(time.DateOnly, input.EffectiveDate)
	zone, _ := time.LoadLocation("America/New_York")
	if err != nil || date.Format(time.DateOnly) > time.Now().In(zone).Format(time.DateOnly) {
		return nil, inventoryInvalid("Choose an effective date no later than today")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = inventoryLock(ctx, tx); err != nil {
		return nil, err
	}
	hash := inventoryHash(input)
	var priorHash string
	var result []InventoryChange
	err = tx.QueryRow(ctx, `SELECT request_hash,result FROM inventory_operations WHERE id=$1`, input.RequestID).Scan(&priorHash, &result)
	if err == nil {
		if hash != priorHash {
			return nil, ErrInventoryConflict
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	changes := append([]InventoryChange(nil), input.Changes...)
	originals := map[string]InventoryRecord{}
	for _, change := range changes {
		if !slices.Contains([]string{"assets", "lines", "equipment"}, change.Collection) {
			return nil, inventoryInvalid("Only devices, lines and supply observations support this operation")
		}
		if _, exists := originals[change.Record.ID]; exists {
			return nil, inventoryInvalid("Select each record only once")
		}
		if err = validateInventory(change.Collection, change.Record); err != nil {
			return nil, err
		}
		old, e := inventoryReadTx(ctx, tx, change.Collection, change.Record.ID)
		if e != nil {
			return nil, e
		}
		if old.Version != change.Record.Version {
			return nil, ErrInventoryConflict
		}
		originals[old.ID] = old
		if change.Collection == "assets" {
			rows, e := tx.Query(ctx, `SELECT id::text FROM inventory_assets WHERE gateway_id=$1 AND coalesce((data->>'active')::boolean,true) UNION ALL SELECT id::text FROM inventory_lines WHERE asset_id=$1 AND coalesce((data->>'active')::boolean,true) ORDER BY 1`, old.ID)
			if e != nil {
				return nil, e
			}
			actual := []string{}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return nil, e
				}
				actual = append(actual, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
			expected := append([]string(nil), input.ExpectedLinks[old.ID]...)
			sort.Strings(expected)
			if !slices.Equal(actual, expected) {
				return nil, ErrInventoryConflict
			}
		}
	}
	// Release the selected edges without changing versions. Deferred guards check
	// the final graph at commit; save below records the original before snapshot.
	for _, change := range changes {
		if change.Collection == "assets" {
			_, err = tx.Exec(ctx, `UPDATE inventory_assets SET data=(data-'truckId'-'gatewayId'-'locationId'-'custodian'-'custodianId') || '{"equipmentState":"unknown"}'::jsonb WHERE id=$1`, change.Record.ID)
		} else if change.Collection == "lines" {
			_, err = tx.Exec(ctx, `UPDATE inventory_lines SET data=data-'assetId' WHERE id=$1`, change.Record.ID)
		}
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.inventory_reason',$1,true),set_config('app.inventory_date',$2,true)`, strings.TrimSpace(input.Reason), input.EffectiveDate); err != nil {
		return nil, err
	}
	rank := func(c InventoryChange) int {
		if c.Collection == "assets" {
			if c.Record.Data.DeviceType == "samsara_gateway" {
				return 0
			}
			return 1
		}
		return 2
	}
	sort.SliceStable(changes, func(i, j int) bool { return rank(changes[i]) < rank(changes[j]) })
	result = []InventoryChange{}
	for _, change := range changes {
		old := originals[change.Record.ID]
		saved, e := inventorySaveTxFrom(ctx, tx, change.Collection, change.Record, actor, inventoryID(), hash, false, &old, input.Verify)
		if e != nil {
			return nil, e
		}
		result = append(result, InventoryChange{change.Collection, saved})
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_operations(id,request_hash,result) VALUES($1,$2,$3)`, input.RequestID, hash, result); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}
