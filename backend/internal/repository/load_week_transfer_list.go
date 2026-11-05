package repository

import (
	"context"
	"strings"
	"time"
)

// Placement metadata only: never add these rows to a statement's payable loads.
type OutgoingLoadTransfer struct {
	StatementID     string `json:"statementId"`
	DriverID        string `json:"driverId"`
	DriverName      string `json:"driverName"`
	TruckID         string `json:"truckId"`
	TruckUnit       string `json:"truckUnit"`
	LoadNumber      string `json:"loadNumber"`
	SourceDate      string `json:"sourceDate"`
	SourceSlot      int    `json:"sourceSlot"`
	DispatchDate    string `json:"dispatchDate"`
	SettlementDate  string `json:"settlementDate"`
	BoardVersion    int    `json:"boardVersion"`
	TransferVersion int    `json:"transferVersion"`
}

// Read independently of prepared statements: a transferred-out load must remain
// manageable even if it was the driver's only load or its destination is hidden.
func (r *DriverPayRepository) OutgoingLoadTransfers(ctx context.Context, kind string, week time.Time, query PayPageQuery) ([]OutgoingLoadTransfer, error) {
	rows, err := r.pool.Query(ctx, `SELECT
 CASE WHEN $2 THEN t.truck_id ELSE payroll_driver_at(t.driver_id,$1::date) END::text,
 t.driver_id::text,d.full_name,coalesce(t.truck_id::text,''),coalesce(tr.unit_number,''),
 e.load_number,t.source_date::text,t.source_slot,t.dispatch_date::text,t.settlement_date::text,e.version,t.version
 FROM gross_board_transfers t
 JOIN `+grossBoardEntriesSQL+` e ON (e.driver_id,e.service_date,e.slot)=(t.driver_id,t.source_date,t.source_slot)
 JOIN drivers d ON d.id=t.driver_id LEFT JOIN trucks tr ON tr.id=t.truck_id
 WHERE t.active AND t.source_date >= $1::date AND t.source_date < $1::date+7
 AND t.settlement_date <> t.source_date AND NOT e.deleted
 AND (NOT $2 OR t.truck_id IS NOT NULL)
 AND ($3='' OR (CASE WHEN $2 THEN t.truck_id ELSE payroll_driver_at(t.driver_id,$1::date) END)::text=$3)
 AND ($4='' OR t.dispatcher_id=$4)
 AND ($5='' OR strpos(lower(concat_ws(' ',d.full_name,tr.unit_number,e.load_number)),lower($5))>0)
 ORDER BY d.full_name,t.source_date,t.source_slot`, week.Format(time.DateOnly), kind == "investor", query.ID, query.DispatcherID, strings.TrimSpace(query.Search))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []OutgoingLoadTransfer{}
	for rows.Next() {
		var item OutgoingLoadTransfer
		if err := rows.Scan(&item.StatementID, &item.DriverID, &item.DriverName, &item.TruckID, &item.TruckUnit, &item.LoadNumber, &item.SourceDate, &item.SourceSlot, &item.DispatchDate, &item.SettlementDate, &item.BoardVersion, &item.TransferVersion); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
