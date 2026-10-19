package repository

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

var inventoryAssetStatuses = []string{"unknown", "active", "inactive", "sold", "total_loss"}
var inventoryLineStatuses = []string{"unknown", "active", "suspended", "cancelled", "pending"}

// Only imports accept provider wording; ordinary writes use the controlled codes.
// Unknown source wording stays in the private snapshot and record notes.
func inventoryImportedStatus(collection, raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "unknown", true
	}
	allowed := inventoryAssetStatuses
	if collection == "lines" {
		allowed = inventoryLineStatuses
	}
	if slices.Contains(allowed, value) {
		return value, true
	}
	if collection == "assets" && value == "total" {
		return "total_loss", true
	}
	if collection == "lines" && (value == "canceled" || strings.HasPrefix(value, "cancelled on ") || strings.HasPrefix(value, "canceled on ")) {
		return "cancelled", true
	}
	return "unknown", false
}

func inventoryImportedFields(collection string, d InventoryFields) InventoryFields {
	if collection != "assets" && collection != "lines" {
		return d
	}
	original := d.Status
	status, known := inventoryImportedStatus(collection, original)
	d.Status = status
	if !known && original != "" && len(d.Notes)+len(original) < 4800 {
		d.Notes = strings.TrimSpace(d.Notes + "\nSource status retained for review: " + original)
	}
	if collection == "assets" {
		switch d.DeviceType {
		case "samsara_camera":
			d.DeviceType, d.Brand = "dashcam", "Samsara"
		case "tt_camera":
			d.DeviceType, d.Brand = "dashcam", "FiveELD"
		}
		if d.TruckID == "" && d.GatewayID == "" && (d.EquipmentState == "reported_present" || d.EquipmentState == "verified_installed") {
			d.EquipmentState = "unknown"
		}
	}
	return d
}

// A linked camera inherits custody, but a gateway transfer does not verify the
// camera's installation on its new truck. Reset it in the same transaction.
func inventoryResetLinkedDashcams(ctx context.Context, tx pgx.Tx, gatewayID string, actor InventoryActor) error {
	rows, err := tx.Query(ctx, `SELECT id::text,version,data,updated_at FROM inventory_assets WHERE gateway_id=$1 AND coalesce((data->>'active')::boolean,true) ORDER BY id`, gatewayID)
	if err != nil {
		return err
	}
	var cameras []InventoryRecord
	for rows.Next() {
		var camera InventoryRecord
		if err = rows.Scan(&camera.ID, &camera.Version, &camera.Data, &camera.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		cameras = append(cameras, camera)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, camera := range cameras {
		if camera.Data.EquipmentState == "unknown" && camera.Data.VerifiedAt == "" {
			continue
		}
		camera.Data.EquipmentState = "unknown"
		_, err = inventorySaveTx(ctx, tx, "assets", camera, actor, inventoryID(), inventoryHash([]any{"gateway-installation-reset", gatewayID, camera.ID, camera.Version}), false)
		if err != nil {
			return err
		}
	}
	return nil
}
