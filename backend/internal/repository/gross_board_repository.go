package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrGrossBoardConflict = errors.New("another dispatcher changed one of these days; reload the board before saving again")
var ErrGrossBoardLoad = errors.New("the selected load no longer matches this load number; select it again")

type GrossBoardRepository struct{ pool *pgxpool.Pool }

func NewGrossBoardRepository(pool *pgxpool.Pool) *GrossBoardRepository {
	return &GrossBoardRepository{pool: pool}
}

type GrossBoardEntry struct {
	DriverID     string `json:"driverId"`
	Date         string `json:"date"`
	LoadNumber   string `json:"loadNumber"`
	LoadRecordID *int   `json:"loadRecordId"`
	OriginalRate string `json:"originalRate"`
	DriverRate   string `json:"driverRate"`
	Miles        string `json:"miles"`
	Version      int    `json:"version"`
}

type GrossBoardDriver struct {
	ID             string `json:"id"`
	FullName       string `json:"fullName"`
	TruckUnit      string `json:"truckUnit"`
	DispatcherID   string `json:"dispatcherId"`
	DispatcherName string `json:"dispatcherName"`
	Active         bool   `json:"active"`
}

type GrossBoard struct {
	WeekStart string             `json:"weekStart"`
	Drivers   []GrossBoardDriver `json:"drivers"`
	Entries   []GrossBoardEntry  `json:"entries"`
}

type GrossBoardLoad struct {
	ID           int    `json:"id"`
	LoadNumber   string `json:"loadNumber"`
	OriginalRate string `json:"originalRate"`
	Miles        string `json:"miles"`
	DriverName   string `json:"driverName"`
	PickupDate   string `json:"pickupDate"`
}

// Resolve an explicit selection, or a unique exact number. Ambiguous numbers
// remain plans until a dispatcher chooses the intended upstream record.
const grossBoardResolvedLoad = `
 LEFT JOIN LATERAL (
   SELECT min(l.id) AS id FROM loads l
   WHERE (e.load_record_id IS NOT NULL AND l.id=e.load_record_id)
      OR (e.load_record_id IS NULL AND e.load_number<>'' AND lower(btrim(l.load_id))=lower(btrim(e.load_number)))
   HAVING count(*)=1
 ) matched ON true
 LEFT JOIN loads l ON l.id=matched.id `

func (r *GrossBoardRepository) Get(ctx context.Context, week time.Time) (GrossBoard, error) {
	board := GrossBoard{WeekStart: week.Format("2006-01-02"), Drivers: []GrossBoardDriver{}, Entries: []GrossBoardEntry{}}
	rows, err := r.pool.Query(ctx, `SELECT d.id, d.full_name, coalesce(t.unit_number,''),
 coalesce(dp.id::text,''), coalesce(dp.full_name,'Unassigned'), d.active
 FROM drivers d LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id
 WHERE d.active OR EXISTS (SELECT 1 FROM gross_board_entries e WHERE e.driver_id=d.id
 AND e.service_date >= $1::date AND e.service_date < $1::date+7)
 ORDER BY dp.full_name NULLS LAST, dp.id, d.full_name, d.id`, week)
	if err != nil {
		return board, err
	}
	for rows.Next() {
		var d GrossBoardDriver
		if err = rows.Scan(&d.ID, &d.FullName, &d.TruckUnit, &d.DispatcherID, &d.DispatcherName, &d.Active); err != nil {
			rows.Close()
			return board, err
		}
		board.Drivers = append(board.Drivers, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return board, err
	}
	rows, err = r.pool.Query(ctx, `SELECT e.driver_id, e.service_date::text,
 CASE WHEN l.id IS NOT NULL THEN l.load_id ELSE e.load_number END, l.id,
 CASE WHEN l.id IS NOT NULL THEN l.total_pay::text ELSE coalesce(e.original_rate::text,'') END,
 coalesce(e.driver_rate::text,''),
 CASE WHEN l.id IS NOT NULL THEN coalesce(l.total_miles::text,'') ELSE coalesce(e.miles::text,'') END,
 e.version FROM gross_board_entries e `+grossBoardResolvedLoad+`
 WHERE e.service_date >= $1::date AND e.service_date < $1::date+7`, week)
	if err != nil {
		return board, err
	}
	defer rows.Close()
	for rows.Next() {
		var e GrossBoardEntry
		if err = rows.Scan(&e.DriverID, &e.Date, &e.LoadNumber, &e.LoadRecordID, &e.OriginalRate, &e.DriverRate, &e.Miles, &e.Version); err != nil {
			return board, err
		}
		board.Entries = append(board.Entries, e)
	}
	return board, rows.Err()
}

func (r *GrossBoardRepository) SearchLoads(ctx context.Context, search string) ([]GrossBoardLoad, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,load_id,total_pay::text,coalesce(total_miles::text,''),
 coalesce(driver_name,''),coalesce((coalesce(pickup_time,pickup_appointment_time) AT TIME ZONE 'UTC')::date::text,'')
 FROM loads WHERE load_id<>'' AND strpos(lower(load_id),lower($1))>0
 ORDER BY (lower(btrim(load_id))=lower(btrim($1))) DESC,id DESC LIMIT 20`, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GrossBoardLoad{}
	for rows.Next() {
		var l GrossBoardLoad
		if err = rows.Scan(&l.ID, &l.LoadNumber, &l.OriginalRate, &l.Miles, &l.DriverName, &l.PickupDate); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

// Save only edited days, atomically, with optimistic concurrency. Empty days
// retain a version so clearing a day cannot allow stale editors to overwrite it.
func (r *GrossBoardRepository) Save(ctx context.Context, entries []GrossBoardEntry) error {
	_, err := r.SaveEntries(ctx, entries)
	return err
}

func (r *GrossBoardRepository) SaveEntries(ctx context.Context, entries []GrossBoardEntry) ([]GrossBoardEntry, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	sort.Slice(entries, func(i, j int) bool { return entries[i].DriverID+entries[i].Date < entries[j].DriverID+entries[j].Date })
	for index, e := range entries {
		var loadID *int
		if e.LoadRecordID != nil {
			var id int
			err = tx.QueryRow(ctx, `SELECT id FROM loads WHERE id=$1 AND lower(btrim(load_id))=lower(btrim($2))`, e.LoadRecordID, e.LoadNumber).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrGrossBoardLoad
			}
			if err != nil {
				return nil, err
			}
			loadID = &id
		} else if e.LoadNumber != "" {
			err = tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(id) END FROM loads WHERE lower(btrim(load_id))=lower(btrim($1))`, e.LoadNumber).Scan(&loadID)
			if err != nil {
				return nil, err
			}
		}
		// Never trust rates or mileage supplied by a browser for a confirmed load.
		original, miles := e.OriginalRate, e.Miles
		if loadID != nil {
			err = tx.QueryRow(ctx, `SELECT total_pay::text,coalesce(total_miles::text,'') FROM loads WHERE id=$1`, loadID).Scan(&original, &miles)
			if err != nil {
				return nil, err
			}
		}
		var version int
		if e.Version == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,driver_rate,miles)
     VALUES($1,$2::date,$3,$4,NULLIF($5,'')::numeric,NULLIF($6,'')::numeric,NULLIF($7,'')::numeric)
     ON CONFLICT DO NOTHING RETURNING version`, e.DriverID, e.Date, e.LoadNumber, loadID, original, e.DriverRate, miles).Scan(&version)
		} else {
			err = tx.QueryRow(ctx, `UPDATE gross_board_entries SET load_number=$3,load_record_id=$4,
     original_rate=NULLIF($5,'')::numeric,driver_rate=NULLIF($6,'')::numeric,miles=NULLIF($7,'')::numeric,
     version=version+1,updated_at=now() WHERE driver_id=$1 AND service_date=$2::date AND version=$8 RETURNING version`,
				e.DriverID, e.Date, e.LoadNumber, loadID, original, e.DriverRate, miles, e.Version).Scan(&version)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGrossBoardConflict
		}
		if err != nil {
			return nil, err
		}
		e.LoadRecordID = loadID
		e.OriginalRate = original
		e.Miles = miles
		e.Version = version
		entries[index] = e
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return entries, nil
}
