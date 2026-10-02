package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDriverBoardConflict = errors.New("driver board or driver home changed; reload to review the latest values before editing again")

type DriverBoardEntry struct {
	DriverID      string `json:"driverId"`
	CurrentLoad   string `json:"currentLoad"`
	TrailerNumber string `json:"trailerNumber"`
	Status        string `json:"status"`
	Destination   string `json:"destination"`
	ETA           string `json:"eta"`
	Notes         string `json:"notes"`
	HomeTime      string `json:"homeTime"`
	DriverHome    string `json:"driverHome"`
	HomeVersion   int    `json:"homeVersion"`
	Version       int    `json:"version"`
}

type DriverBoardDriver struct {
	ID             string `json:"id"`
	FullName       string `json:"fullName"`
	DriverType     string `json:"driverType"`
	TruckUnit      string `json:"truckUnit"`
	Phone          string `json:"phone"`
	DispatcherID   string `json:"dispatcherId"`
	DispatcherName string `json:"dispatcherName"`
}

type DriverBoard struct {
	WeekStart    string              `json:"weekStart"`
	Drivers      []DriverBoardDriver `json:"drivers"`
	Entries      []DriverBoardEntry  `json:"entries"`
	GrossEntries []GrossBoardEntry   `json:"grossEntries"`
}

type DriverBoardRepository struct{ pool *pgxpool.Pool }

func NewDriverBoardRepository(pool *pgxpool.Pool) *DriverBoardRepository {
	return &DriverBoardRepository{pool: pool}
}

// Ownership and pay method are independent. An owner driving their own truck
// is not a hired driver on an investor truck, regardless of their profile flag.
func driverBoardType(payType string, ownerOperator, investorTruck, ownTruck bool) string {
	if investorTruck && !ownTruck {
		if payType == "cpm" {
			return "M-O"
		}
		return "%-O"
	}
	if payType == "cpm" {
		return "M"
	}
	if ownerOperator || ownTruck {
		return "O"
	}
	return "%"
}

func (r *DriverBoardRepository) Get(ctx context.Context, week time.Time) (DriverBoard, error) {
	result := DriverBoard{WeekStart: week.Format("2006-01-02"), Drivers: []DriverBoardDriver{}, Entries: []DriverBoardEntry{}, GrossEntries: []GrossBoardEntry{}}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT d.id,d.full_name,d.pay_type,d.is_owner_operator,
 coalesce(NOT i.is_company,false),coalesce(i.driver_id=d.id,false),coalesce(t.unit_number,''),coalesce(d.phone,''),
 coalesce(dp.id::text,''),coalesce(dp.full_name,'Unassigned'),
 coalesce(b.current_load,''),coalesce(b.trailer_number,''),coalesce(b.status,''),coalesce(b.destination,''),
 coalesce(b.eta,''),coalesce(b.notes,''),coalesce(b.home_time,''),d.driver_home,d.driver_home_version,coalesce(b.version,0)
 FROM drivers d LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id LEFT JOIN investors i ON i.id=t.owner_id
 LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.active
 ORDER BY dp.full_name NULLS LAST,dp.id,d.full_name,d.id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var d DriverBoardDriver
		var e DriverBoardEntry
		var pay string
		var owner, investor, own bool
		err = rows.Scan(&d.ID, &d.FullName, &pay, &owner, &investor, &own, &d.TruckUnit, &d.Phone, &d.DispatcherID, &d.DispatcherName,
			&e.CurrentLoad, &e.TrailerNumber, &e.Status, &e.Destination, &e.ETA, &e.Notes, &e.HomeTime, &e.DriverHome, &e.HomeVersion, &e.Version)
		if err != nil {
			rows.Close()
			return result, err
		}
		d.DriverType = driverBoardType(pay, owner, investor, own)
		e.DriverID = d.ID
		result.Drivers = append(result.Drivers, d)
		result.Entries = append(result.Entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	// Use the same exact load resolution and entered/source precedence as Gross Board.
	rows, err = tx.Query(ctx, grossBoardEffective+"SELECT "+grossBoardEntryColumns+` FROM effective
 WHERE service_date >= $1::date AND driver_id IN (SELECT id FROM drivers WHERE active)
 ORDER BY driver_id,service_date,slot`, week)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var e GrossBoardEntry
		if err = scanGrossBoardEntry(rows, &e); err != nil {
			rows.Close()
			return result, err
		}
		result.GrossEntries = append(result.GrossEntries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *DriverBoardRepository) Save(ctx context.Context, entries []DriverBoardEntry, actor ...string) ([]DriverBoardEntry, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	user := ""
	if len(actor) > 0 {
		user = actor[0]
	}
	if err = setBoardActor(ctx, tx, user, "board"); err != nil {
		return nil, err
	}
	entries, err = saveDriverBoardEntries(ctx, tx, entries)
	if err != nil {
		return nil, err
	}
	return entries, tx.Commit(ctx)
}

func saveDriverBoardEntries(ctx context.Context, tx pgx.Tx, entries []DriverBoardEntry) ([]DriverBoardEntry, error) {
	var err error
	entries = append([]DriverBoardEntry(nil), entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].DriverID < entries[j].DriverID })
	for index := range entries {
		e := &entries[index]
		var home string
		var homeVersion int
		var active bool
		err = tx.QueryRow(ctx, `SELECT driver_home,driver_home_version,active FROM drivers WHERE id=$1 FOR UPDATE`, e.DriverID).Scan(&home, &homeVersion, &active)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDriverBoardConflict
		}
		if err != nil {
			return nil, err
		}
		if !active || homeVersion != e.HomeVersion {
			return nil, ErrDriverBoardConflict
		}
		var version int
		err = tx.QueryRow(ctx, `INSERT INTO driver_board(driver_id,current_load,trailer_number,status,destination,eta,notes,home_time)
   SELECT $1,$2,$3,$4,$5,$6,$7,$8 WHERE $9::int=0
   ON CONFLICT(driver_id) DO NOTHING RETURNING version`, e.DriverID, e.CurrentLoad, e.TrailerNumber, e.Status, e.Destination, e.ETA, e.Notes, e.HomeTime, e.Version).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) && e.Version > 0 {
			err = tx.QueryRow(ctx, `UPDATE driver_board SET current_load=$2,trailer_number=$3,status=$4,destination=$5,eta=$6,notes=$7,home_time=$8,
    version=version+1,updated_at=now() WHERE driver_id=$1 AND version=$9 RETURNING version`,
				e.DriverID, e.CurrentLoad, e.TrailerNumber, e.Status, e.Destination, e.ETA, e.Notes, e.HomeTime, e.Version).Scan(&version)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDriverBoardConflict
		}
		if err != nil {
			return nil, err
		}
		if home != e.DriverHome {
			err = tx.QueryRow(ctx, `UPDATE drivers SET driver_home=$2,updated_at=now() WHERE id=$1 RETURNING driver_home_version`, e.DriverID, e.DriverHome).Scan(&e.HomeVersion)
			if err != nil {
				return nil, err
			}
		}
		e.Version = version
	}
	return entries, nil
}
