package repository

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type PayDispatcherComparison struct {
	DataTruck string `json:"dataTruck"`
	Clustie   string `json:"clustie"`
	Similar   bool   `json:"similar"`
}

func comparePayDispatcherNames(dataTruck, clustie string) *PayDispatcherComparison {
	dataTruck, clustie = strings.TrimSpace(dataTruck), strings.TrimSpace(clustie)
	providerTokens, nativeTokens := personNameTokens(dataTruck), personNameTokens(clustie)
	if len(providerTokens) == 0 || len(nativeTokens) == 0 || strings.EqualFold(dataTruck, "Unassigned") ||
		strings.EqualFold(clustie, "Unassigned") || slices.Equal(providerTokens, nativeTokens) {
		return nil
	}
	similar := relayDriverNameMatchQuality(dataTruck, clustie) >= 80 ||
		dispatcherFirstInitialMatches(providerTokens, nativeTokens) || dispatcherFirstInitialMatches(nativeTokens, providerTokens)
	return &PayDispatcherComparison{DataTruck: dataTruck, Clustie: clustie, Similar: similar}
}

func dispatcherFirstInitialMatches(abbreviated, full []string) bool {
	return len(abbreviated) == 2 && len(full) == 2 && abbreviated[1] == full[1] &&
		utf8.RuneCountInString(abbreviated[0]) == 1 && strings.HasPrefix(full[0], abbreviated[0])
}

// Add current comparison evidence only to returned load rows. Financial calculations,
// prepared revisions and frozen statement bodies do not contain this decoration.
func decoratePayDispatcherComparisons(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	type loadReference struct {
		payAssignmentDay
		LoadID    int `json:"load_id"`
		Statement int `json:"statement_index"`
		Row       int `json:"row_index"`
	}
	var refs []loadReference
	for i, d := range report.Drivers {
		for j, load := range d.Loads {
			if load.LoadRecordID == nil {
				continue
			}
			id, date := payLoadAssignmentDay(d.ID, kind, load)
			if id != "" && date != "" {
				refs = append(refs, loadReference{payAssignmentDay{id, date}, *load.LoadRecordID, i, j})
			}
		}
	}
	if len(refs) == 0 {
		return nil
	}
	encoded, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT r.statement_index,r.row_index,l.dispatcher_name,a.dispatcher_name
 FROM jsonb_to_recordset($1::jsonb) r(driver uuid,date date,load_id int,statement_index int,row_index int)
 JOIN loads l ON l.id=r.load_id AND nullif(btrim(l.dispatcher_name),'') IS NOT NULL AND `+boardAvailableSourceSQL+`
 CROSS JOIN LATERAL (
  SELECT min(coalesce(p.full_name,h.dispatcher_name)) dispatcher_name
  FROM driver_dispatcher_assignments h LEFT JOIN dispatchers p ON p.id=h.dispatcher_id
  WHERE h.driver_id=payroll_driver_at(r.driver,r.date)
  AND (NOT h.start_known OR h.assigned_at<((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
  AND (h.unassigned_at IS NULL OR h.unassigned_at>=((r.date+1)::timestamp AT TIME ZONE 'America/New_York'))
  AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
  HAVING count(*)=1
 ) a`, string(encoded))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var i, j int
		var dataTruck, clustie string
		if err = rows.Scan(&i, &j, &dataTruck, &clustie); err != nil {
			return err
		}
		report.Drivers[i].Loads[j].DispatcherComparison = comparePayDispatcherNames(dataTruck, clustie)
	}
	return rows.Err()
}
