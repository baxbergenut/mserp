package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

type MaintenanceTruckChoice struct {
	ID             string  `json:"id"`
	UnitNumber     string  `json:"unitNumber"`
	Active         bool    `json:"active"`
	OwnerID        *string `json:"ownerId"`
	OwnerName      string  `json:"ownerName"`
	OwnerDriverID  *string `json:"ownerDriverId"`
	IsCompanyOwned bool    `json:"isCompanyOwned"`
	DriverID       *string `json:"driverId"`
	DriverName     string  `json:"driverName"`
}

func (r *MaintenanceRepository) Choices(ctx context.Context) ([]MaintenanceTruckChoice, []string, error) {
	users := []string{}
	if err := r.pool.QueryRow(ctx, "SELECT coalesce(array_agg(username ORDER BY username),'{}') FROM app_users WHERE active").Scan(&users); err != nil {
		return nil, nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT t.id,t.unit_number,t.active,t.owner_id,coalesce(od.full_name,i.full_name,''),i.driver_id,t.is_company_owned,a.driver_id,coalesce(d.full_name,'') FROM trucks t LEFT JOIN investors i ON i.id=t.owner_id LEFT JOIN drivers od ON od.id=i.driver_id LEFT JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL LEFT JOIN drivers d ON d.id=a.driver_id ORDER BY t.unit_number`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	trucks := []MaintenanceTruckChoice{}
	for rows.Next() {
		var v MaintenanceTruckChoice
		if err := rows.Scan(&v.ID, &v.UnitNumber, &v.Active, &v.OwnerID, &v.OwnerName, &v.OwnerDriverID, &v.IsCompanyOwned, &v.DriverID, &v.DriverName); err != nil {
			return nil, nil, err
		}
		trucks = append(trucks, v)
	}
	return trucks, users, rows.Err()
}
func (r *MaintenanceRepository) CreateDocument(ctx context.Context, record, name, kind, actor string, data []byte) (MaintenanceDocument, error) {
	sum := sha256.Sum256(data)
	var v MaintenanceDocument
	err := r.pool.QueryRow(ctx, `INSERT INTO maintenance_documents(record_id,file_name,content_type,size_bytes,sha256,data,uploaded_by) SELECT id,$2,$3,$4,$5,$6,$7 FROM maintenance_records WHERE id=$1 AND status<>'voided' ON CONFLICT(record_id,sha256,file_name) DO UPDATE SET file_name=excluded.file_name RETURNING id,file_name,content_type,size_bytes,created_at`, record, name, kind, len(data), hex.EncodeToString(sum[:]), data, actor).Scan(&v.ID, &v.FileName, &v.ContentType, &v.SizeBytes, &v.CreatedAt)
	return v, mapNotFound(err)
}
func (r *MaintenanceRepository) Document(ctx context.Context, id string) (MaintenanceDocument, []byte, error) {
	var v MaintenanceDocument
	var data []byte
	err := r.pool.QueryRow(ctx, `SELECT id,file_name,content_type,size_bytes,created_at,data FROM maintenance_documents WHERE id=$1`, id).Scan(&v.ID, &v.FileName, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &data)
	return v, data, mapNotFound(err)
}

func (r *MaintenanceRepository) OwnedBy(ctx context.Context, id, actor string) (bool, error) {
	var own bool
	err := r.pool.QueryRow(ctx, "SELECT coalesce(created_by=$2::uuid,false) FROM maintenance_records WHERE id=$1", id, actor).Scan(&own)
	return own, mapNotFound(err)
}
