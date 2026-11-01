package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

type payrollTruckInputScope struct {
	Week   string   `json:"week"`
	Trucks []string `json:"trucks"`
}

func payrollTruckInputKey(week string, trucks []string, costsOnly bool) string {
	key, _ := json.Marshal(struct {
		Week      string
		Trucks    []string
		CostsOnly bool
	}{week, trucks, costsOnly})
	return string(key)
}

// Read the original period-specific queries together after canonical writes.
// The independent single-week reader remains the reference; scopes are never
// merged across dates, and callers receive copies of their exact selection.
func preparePayrollTruckInputs(ctx context.Context, tx pgx.Tx, scopes []payrollTruckInputScope) error {
	publication, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	if publication == nil || publication.tx != tx || len(scopes) < 2 {
		return nil
	}
	body, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	values := map[string]*truckPayInputs{}
	for _, scope := range scopes {
		values[scope.Week] = &truckPayInputs{metadata: map[string]truckPayMetadata{}, assigned: map[string]map[string]bool{}, edits: map[string]investorWeekEdit{}, expenses: map[string][]truckExpense{}}
	}
	for _, part := range []struct{ kind, query string }{
		{"metadata", payrollTruckMetadataSQL}, {"assigned", payrollTruckAssignmentsSQL}, {"edits", payrollTruckEditsSQL}, {"expenses", payrollTruckExpensesSQL},
	} {
		query := strings.ReplaceAll(part.query, "$1::date", "payroll_scope.week")
		query = strings.ReplaceAll(query, "$2::uuid[]", "payroll_scope.trucks")
		query = `SELECT payroll_scope.week::text,x.* FROM jsonb_to_recordset($1::jsonb) payroll_scope(week date,trucks uuid[]) CROSS JOIN LATERAL (` + query + `) x`
		if part.kind == "expenses" {
			query += ` ORDER BY 1,7,4`
		}
		rows, err := tx.Query(ctx, query, body)
		if err != nil {
			return err
		}
		for rows.Next() {
			var week, id string
			switch part.kind {
			case "metadata":
				var m truckPayMetadata
				var ownerFull *bool
				err = rows.Scan(&week, &id, &m.unit, &m.name, &m.ownerDriver, &m.conflict, &ownerFull, &m.feeOwnerMatches)
				if err == nil {
					m.ownerFullWeek = ownerFull != nil && *ownerFull
					values[week].metadata[id] = m
				}
			case "assigned":
				var driver string
				err = rows.Scan(&week, &id, &driver)
				if err == nil {
					if values[week].assigned[id] == nil {
						values[week].assigned[id] = map[string]bool{}
					}
					values[week].assigned[id][driver] = true
				}
			case "edits":
				var w investorWeekEdit
				err = rows.Scan(&week, &id, &w.edits, &w.meta.Version, &w.meta.Finalized, &w.report, &w.meta.FinalizedAt, &w.meta.FinalizedBy)
				if err == nil {
					values[week].edits[id] = w
				}
			case "expenses":
				var x truckExpense
				d := &x.deduction
				err = rows.Scan(&week, &id, &x.destination, &d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved)
				if err == nil {
					values[week].expenses[id] = append(values[week].expenses[id], x)
				}
			}
			if err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, payrollTruckPeopleSQL)
	if err != nil {
		return err
	}
	var people []truckPayPerson
	for rows.Next() {
		var p truckPayPerson
		if err = rows.Scan(&p.person.ID, &p.person.Name, &p.dispatcherID, &p.dispatcherName); err != nil {
			rows.Close()
			return err
		}
		people = append(people, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, scope := range scopes {
		value := *values[scope.Week]
		value.people = people
		publication.truckInputs[payrollTruckInputKey(scope.Week, scope.Trucks, false)] = copyTruckPayInputs(value)
		value.expenses = map[string][]truckExpense{}
		publication.truckInputs[payrollTruckInputKey(scope.Week, scope.Trucks, true)] = copyTruckPayInputs(value)
	}
	return nil
}
