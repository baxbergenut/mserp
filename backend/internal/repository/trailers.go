package repository

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

var trailerStatuses = []string{"ready", "shop", "in_use", "out_of_service", "terminated"}

func trailerRestricted(status string) bool {
	return status == "shop" || status == "out_of_service" || status == "terminated"
}

func prepareTrailerSave(ctx context.Context, tx pgx.Tx, v *InventoryRecord, old InventoryRecord, exists, importing bool, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		return inventoryInvalid("Reason must be 1000 characters or fewer")
	}
	if importing {
		// A source sheet cannot establish operational readiness or replace a crew link.
		if exists {
			v.Data.Status = old.Data.Status
			v.Data.TruckID = old.Data.TruckID
			v.Data.StatusReason = old.Data.StatusReason
		} else {
			v.Data.Status = "shop"
			v.Data.TruckID = ""
			reason = "Imported trailer requires operational review; source notes are preserved."
		}
	}
	if v.Data.Status == "" && !exists {
		v.Data.Status = "ready"
	}
	if !slices.Contains(trailerStatuses, v.Data.Status) {
		return inventoryInvalid("Choose Ready, In use, Shop, OOS or Terminated")
	}
	if exists {
		v.Data.Condition = old.Data.Condition
	} // Legacy condition is historical, not independently editable.
	changedTruck := v.Data.TruckID != old.Data.TruckID
	if v.Data.TruckID != "" {
		if !inventoryUUID.MatchString(v.Data.TruckID) {
			return inventoryInvalid("Select a valid truck")
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT unit_number,fleet_truck_operating_status(id) FROM trucks WHERE id=$1`, v.Data.TruckID).Scan(&v.TruckUnit, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return inventoryInvalid("Assigned truck was not found")
			}
			return err
		}
		if changedTruck {
			if trailerRestricted(old.Data.Status) || trailerRestricted(v.Data.Status) {
				return inventoryInvalid("Return the trailer to service before assigning another truck")
			}
			if status != "ready" && status != "in_use" {
				return inventoryInvalid("Return the truck to service before assigning a trailer")
			}
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleet_trailers WHERE truck_id=$1 AND id<>$2)`, v.Data.TruckID, v.ID).Scan(&used); err != nil {
				return err
			}
			if used {
				return inventoryInvalid("This truck already has a trailer; release that assignment first")
			}
		}
	} else {
		v.TruckUnit = ""
	}
	if v.Data.Status == "terminated" && (v.Data.TruckID != "" || old.Data.TruckID != "") {
		return inventoryInvalid("End the truck assignment before terminating this trailer")
	}
	if !trailerRestricted(v.Data.Status) {
		if v.Data.Status == "in_use" && v.Data.TruckID == "" && !changedTruck {
			return inventoryInvalid("Assign a truck to put the trailer In use")
		}
		v.Data.Status = "ready"
		if v.Data.TruckID != "" {
			v.Data.Status = "in_use"
		}
	}
	changedStatus := v.Data.Status != old.Data.Status
	if (changedStatus && (trailerRestricted(v.Data.Status) || trailerRestricted(old.Data.Status))) && reason == "" {
		return inventoryInvalid("A reason is required to restrict or return a trailer to service")
	}
	if changedStatus || changedTruck || !exists {
		if !changedStatus && trailerRestricted(v.Data.Status) && reason == "" {
			reason = old.Data.StatusReason
		}
		if reason == "" && changedTruck {
			if v.Data.TruckID != "" {
				reason = "Assigned to truck " + v.TruckUnit
			} else {
				reason = "Truck assignment ended"
			}
		}
		v.Data.StatusReason = reason
	} else {
		v.Data.StatusReason = old.Data.StatusReason
		if reason != "" {
			v.Data.StatusReason = reason
		}
	}
	v.Data.Active = v.Data.Status != "out_of_service" && v.Data.Status != "terminated"
	return nil
}

const selectTrailersSQL = `SELECT r.id::text,r.version,r.data,r.updated_at,coalesce(u.username,''),coalesce(t.unit_number,'') FROM fleet_trailers r LEFT JOIN app_users u ON u.id=r.updated_by LEFT JOIN trucks t ON t.id=r.truck_id`

func (r *InventoryRepository) listTrailers(ctx context.Context, id string) ([]InventoryRecord, error) {
	rows, err := r.pool.Query(ctx, selectTrailersSQL+` WHERE ($1='' OR r.id::text=$1) ORDER BY r.data->>'unitNumber',r.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []InventoryRecord{}
	for rows.Next() {
		var v InventoryRecord
		if err = rows.Scan(&v.ID, &v.Version, &v.Data, &v.UpdatedAt, &v.UpdatedBy, &v.TruckUnit); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r *InventoryRepository) Trailer(ctx context.Context, id string) (InventoryRecord, error) {
	values, err := r.listTrailers(ctx, id)
	if err != nil {
		return InventoryRecord{}, err
	}
	if len(values) == 0 {
		return InventoryRecord{}, pgx.ErrNoRows
	}
	return values[0], nil
}

type TrailerPage struct {
	Page[InventoryRecord]
	StatusCounts     map[string]int `json:"statusCounts"`
	StorageLocations []string       `json:"storageLocations"`
}
type TrailerFilters struct{ Search, Status, Storage, TruckID string }

func (r *InventoryRepository) TrailersPage(ctx context.Context, p Pagination, f TrailerFilters) (TrailerPage, error) {
	if f.Status != "" && !slices.Contains(trailerStatuses, f.Status) {
		return TrailerPage{}, inventoryInvalid("Invalid trailer status filter")
	}
	if f.TruckID != "" && !inventoryUUID.MatchString(f.TruckID) {
		return TrailerPage{}, inventoryInvalid("Invalid truck filter")
	}
	records, err := r.listTrailers(ctx, "")
	if err != nil {
		return TrailerPage{}, err
	}
	return paginateTrailers(records, p, f), nil
}

// Stored-trailer rosters are small; do the filtering/counting on the server while
// retaining the compatible unpaginated roster for existing inventory readers.
func paginateTrailers(records []InventoryRecord, p Pagination, f TrailerFilters) TrailerPage {
	counts := map[string]int{}
	for _, status := range trailerStatuses {
		counts[status] = 0
	}
	locations := []string{}
	filtered := []InventoryRecord{}
	query := strings.ToLower(strings.TrimSpace(f.Search))
	for _, v := range records {
		d := v.Data
		if d.StorageLocation != "" && !slices.Contains(locations, d.StorageLocation) {
			locations = append(locations, d.StorageLocation)
		}
		if f.Storage != "" && d.StorageLocation != f.Storage || f.TruckID != "" && d.TruckID != f.TruckID {
			continue
		}
		text := strings.Join([]string{d.UnitNumber, d.VIN, d.Year, d.Make, d.Plate, d.PlateState, d.StorageLocation, d.Usage, d.Notes, d.StatusReason, v.TruckUnit}, " ")
		if !strings.Contains(strings.ToLower(text), query) {
			continue
		}
		counts[d.Status]++
		if f.Status == "" || d.Status == f.Status {
			filtered = append(filtered, v)
		}
	}
	sort.Strings(locations)
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		x, y := slices.Index(trailerStatuses, a.Data.Status), slices.Index(trailerStatuses, b.Data.Status)
		if x != y {
			return x < y
		}
		if a.Data.UnitNumber != b.Data.UnitNumber {
			return a.Data.UnitNumber < b.Data.UnitNumber
		}
		return a.ID < b.ID
	})
	total := len(filtered)
	p = p.Normalize(total)
	end := min(total, p.Offset()+p.PageSize)
	return TrailerPage{Page: NewPage(filtered[p.Offset():end], total, p), StatusCounts: counts, StorageLocations: locations}
}
