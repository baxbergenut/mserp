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
	DriverID            string `json:"driverId"`
	Date                string `json:"date"`
	LoadNumber          string `json:"loadNumber"`
	DayStatus           string `json:"dayStatus"`
	LoadRecordID        *int   `json:"loadRecordId"`
	OriginalRate        string `json:"originalRate"`
	DriverRate          string `json:"driverRate"`
	Miles               string `json:"miles"`
	Version             int    `json:"version"`
	EnteredOriginalRate string `json:"enteredOriginalRate"`
	EnteredMiles        string `json:"enteredMiles"`
	Duplicate           bool   `json:"duplicate"`
	AcceptSystemValues  bool   `json:"acceptSystemValues,omitempty"`
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
	WeekStart string              `json:"weekStart"`
	Drivers   []GrossBoardDriver  `json:"drivers"`
	Entries   []GrossBoardEntry   `json:"entries"`
	Balances  []GrossBoardBalance `json:"balances"`
}

type GrossBoardBalance struct {
	DriverID          string `json:"driverId"`
	OpeningBalance    string `json:"openingBalance"`
	OpeningIncomplete int    `json:"openingIncomplete"`
}

type GrossBoardBalanceLine struct {
	Date         string `json:"date"`
	LoadNumber   string `json:"loadNumber"`
	OriginalRate string `json:"originalRate"`
	DriverRate   string `json:"driverRate"`
	Change       string `json:"change"`
	Balance      string `json:"balance"`
	Duplicate    bool   `json:"duplicate"`
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
   WHERE e.day_status='' AND ((e.load_record_id IS NOT NULL AND l.id=e.load_record_id)
      OR (e.load_record_id IS NULL AND e.load_number<>'' AND lower(btrim(l.load_id))=lower(btrim(e.load_number))))
   HAVING count(*)=1
 ) matched ON true
 LEFT JOIN loads l ON l.id=matched.id `

// Every read uses current source values. Carry is calculated from the same
// effective entries as the grid, with exact numeric arithmetic. A repeated
// system load for one driver contributes only on its earliest board date.
const grossBoardEffective = `WITH resolved AS (
 SELECT e.*, l.id AS matched_id,
 CASE WHEN l.id IS NOT NULL THEN l.load_id ELSE e.load_number END AS display_number,
 CASE WHEN l.id IS NOT NULL THEN l.total_pay ELSE e.original_rate END AS effective_original,
 CASE WHEN l.id IS NOT NULL THEN l.total_miles ELSE e.miles END AS effective_miles,
 l.id IS NOT NULL AND row_number() OVER (
   PARTITION BY e.driver_id, l.id ORDER BY e.service_date) > 1 AS duplicate
 FROM gross_board_entries e ` + grossBoardResolvedLoad + `
 WHERE e.service_date < $1::date+7
), effective AS (
 SELECT *, CASE WHEN day_status='' AND NOT duplicate AND btrim(load_number)<>'' AND
 effective_original IS NOT NULL AND driver_rate IS NOT NULL
 THEN effective_original-driver_rate END AS balance_change
 FROM resolved
) `

const grossBoardEntryColumns = `driver_id, service_date::text, display_number,
 matched_id, coalesce(effective_original::text,''), coalesce(driver_rate::text,''),
 coalesce(effective_miles::text,''), version, coalesce(entered_original_rate::text,''),
 coalesce(entered_miles::text,''), duplicate, day_status`

func scanGrossBoardEntry(row pgx.Row, e *GrossBoardEntry) error {
	return row.Scan(&e.DriverID, &e.Date, &e.LoadNumber, &e.LoadRecordID,
		&e.OriginalRate, &e.DriverRate, &e.Miles, &e.Version,
		&e.EnteredOriginalRate, &e.EnteredMiles, &e.Duplicate, &e.DayStatus)
}

func (r *GrossBoardRepository) Get(ctx context.Context, week time.Time) (GrossBoard, error) {
	board := GrossBoard{WeekStart: week.Format("2006-01-02"), Drivers: []GrossBoardDriver{}, Entries: []GrossBoardEntry{}, Balances: []GrossBoardBalance{}}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return board, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT d.id, d.full_name, coalesce(t.unit_number,''),
 coalesce(dp.id::text,''), coalesce(dp.full_name,'Unassigned'), d.active
 FROM drivers d LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id
 WHERE d.active OR EXISTS (SELECT 1 FROM gross_board_entries e WHERE e.driver_id=d.id
 AND e.service_date < $1::date+7)
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

	rows, err = tx.Query(ctx, grossBoardEffective+"SELECT "+grossBoardEntryColumns+`
 FROM effective WHERE service_date >= $1::date ORDER BY driver_id,service_date`, week)
	if err != nil {
		return board, err
	}
	for rows.Next() {
		var e GrossBoardEntry
		if err = scanGrossBoardEntry(rows, &e); err != nil {
			rows.Close()
			return board, err
		}
		board.Entries = append(board.Entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return board, err
	}
	rows, err = tx.Query(ctx, grossBoardEffective+`SELECT driver_id,
 coalesce(sum(balance_change),0)::text,
 count(*) FILTER (WHERE day_status='' AND balance_change IS NULL AND NOT duplicate
 AND (load_number<>'' OR effective_original IS NOT NULL OR driver_rate IS NOT NULL))::int
 FROM effective WHERE service_date < $1::date GROUP BY driver_id`, week)
	if err != nil {
		return board, err
	}
	defer rows.Close()
	for rows.Next() {
		var balance GrossBoardBalance
		if err = rows.Scan(&balance.DriverID, &balance.OpeningBalance, &balance.OpeningIncomplete); err != nil {
			return board, err
		}
		board.Balances = append(board.Balances, balance)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return board, err
	}
	return board, tx.Commit(ctx)
}

func (r *GrossBoardRepository) BalanceHistory(ctx context.Context, driverID string, week time.Time) ([]GrossBoardBalanceLine, error) {
	rows, err := r.pool.Query(ctx, grossBoardEffective+`SELECT service_date::text, display_number,
 coalesce(effective_original::text,''), coalesce(driver_rate::text,''),
 coalesce(balance_change::text,''),
 coalesce(sum(balance_change) OVER (ORDER BY service_date ROWS UNBOUNDED PRECEDING),0)::text, duplicate
 FROM effective WHERE driver_id=$2 AND day_status='' AND
 (load_number<>'' OR effective_original IS NOT NULL OR driver_rate IS NOT NULL)
 ORDER BY service_date`, week, driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GrossBoardBalanceLine{}
	for rows.Next() {
		var line GrossBoardBalanceLine
		if err = rows.Scan(&line.Date, &line.LoadNumber, &line.OriginalRate, &line.DriverRate, &line.Change, &line.Balance, &line.Duplicate); err != nil {
			return nil, err
		}
		result = append(result, line)
	}
	return result, rows.Err()
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

		enteredOriginal, enteredMiles := e.EnteredOriginalRate, e.EnteredMiles
		if enteredOriginal == "" {
			enteredOriginal = e.OriginalRate
		}
		if enteredMiles == "" {
			enteredMiles = e.Miles
		}
		if e.Version > 0 && loadID != nil {
			var previousNumber, previousOriginal, previousMiles string
			var previousID *int
			err = tx.QueryRow(ctx, `SELECT load_number,load_record_id,
              coalesce(entered_original_rate::text,original_rate::text,''),
              coalesce(entered_miles::text,miles::text,'')
              FROM gross_board_entries WHERE driver_id=$1 AND service_date=$2::date AND version=$3 FOR UPDATE`,
				e.DriverID, e.Date, e.Version).Scan(&previousNumber, &previousID, &previousOriginal, &previousMiles)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrGrossBoardConflict
			}
			if err != nil {
				return nil, err
			}
			// A linked entry keeps its reference until explicitly reviewed.
			// A plan keeps its entered amounts even if its text is replaced by a system number.
			if previousID == nil || *previousID == *loadID {
				if e.EnteredOriginalRate == "" && previousOriginal != "" {
					enteredOriginal = previousOriginal
				}
				if e.EnteredMiles == "" && previousMiles != "" {
					enteredMiles = previousMiles
				}
			}
		}
		// Never trust rates or mileage supplied by a browser for a confirmed load.
		original, miles := e.OriginalRate, e.Miles
		if loadID != nil {
			err = tx.QueryRow(ctx, `SELECT total_pay::text,coalesce(total_miles::text,'') FROM loads WHERE id=$1 FOR SHARE`, loadID).Scan(&original, &miles)
			if err != nil {
				return nil, err
			}
		}
		if loadID == nil {
			enteredOriginal, enteredMiles = original, miles
		} else if e.AcceptSystemValues {
			// Do not acknowledge a source value that changed since the user saw it.
			var unchanged bool
			err = tx.QueryRow(ctx, `SELECT total_pay IS NOT DISTINCT FROM NULLIF($2,'')::numeric
              AND total_miles IS NOT DISTINCT FROM NULLIF($3,'')::numeric FROM loads WHERE id=$1`,
				loadID, e.OriginalRate, e.Miles).Scan(&unchanged)
			if err != nil {
				return nil, err
			}
			if !unchanged {
				return nil, ErrGrossBoardConflict
			}
			enteredOriginal, enteredMiles = original, miles
		}
		var version int
		if e.Version == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,driver_rate,miles,entered_original_rate,entered_miles,day_status)
     VALUES($1,$2::date,$3,$4,NULLIF($5,'')::numeric,NULLIF($6,'')::numeric,NULLIF($7,'')::numeric,NULLIF($8,'')::numeric,NULLIF($9,'')::numeric,$10)
     ON CONFLICT DO NOTHING RETURNING version`, e.DriverID, e.Date, e.LoadNumber, loadID, original, e.DriverRate, miles, enteredOriginal, enteredMiles, e.DayStatus).Scan(&version)
		} else {
			err = tx.QueryRow(ctx, `UPDATE gross_board_entries SET load_number=$3,load_record_id=$4,
     original_rate=NULLIF($5,'')::numeric,driver_rate=NULLIF($6,'')::numeric,miles=NULLIF($7,'')::numeric,
     entered_original_rate=NULLIF($9,'')::numeric,entered_miles=NULLIF($10,'')::numeric,day_status=$11,
     version=version+1,updated_at=now() WHERE driver_id=$1 AND service_date=$2::date AND version=$8 RETURNING version`,
				e.DriverID, e.Date, e.LoadNumber, loadID, original, e.DriverRate, miles, e.Version, enteredOriginal, enteredMiles, e.DayStatus).Scan(&version)
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
		e.EnteredOriginalRate, e.EnteredMiles = enteredOriginal, enteredMiles
		e.AcceptSystemValues = false
		entries[index] = e
	}

	for i := range entries {
		date, err := time.Parse(time.DateOnly, entries[i].Date)
		if err != nil {
			return nil, err
		}
		err = scanGrossBoardEntry(tx.QueryRow(ctx, grossBoardEffective+"SELECT "+grossBoardEntryColumns+
			" FROM effective WHERE driver_id=$2 AND service_date=$3::date", date, entries[i].DriverID, entries[i].Date), &entries[i])
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return entries, nil
}
