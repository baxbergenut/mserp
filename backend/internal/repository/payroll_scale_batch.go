package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

type payrollScaleWeek [2]map[string][]PayAutoCharge

func payrollScaleCache(ctx context.Context, tx pgx.Tx) map[string]payrollScaleWeek {
	if authoritative, _ := ctx.Value(payrollAuthoritativeKey{}).(bool); authoritative {
		return nil
	}
	if read := payrollReadMemo(ctx, tx); read != nil && read.scale != nil {
		return read.scale
	}
	if publication, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState); publication != nil && publication.tx == tx {
		return publication.scale
	}
	return nil
}

func copyPayrollScaleWeek(value payrollScaleWeek) payrollScaleWeek {
	copy := payrollScaleWeek{{}, {}}
	for i, rows := range value {
		for id, charges := range rows {
			copy[i][id] = append([]PayAutoCharge{}, charges...)
		}
	}
	return copy
}

// Cache the complete fleet's scale charges within the post-write read phase.
func preparePayrollScaleWeeks(ctx context.Context, tx pgx.Tx, weeks []string) error {
	cache := payrollScaleCache(ctx, tx)
	if cache == nil || len(weeks) < 2 {
		return nil
	}
	values, err := readPayrollScaleWeeks(ctx, tx, weeks)
	if err != nil {
		return err
	}
	for week, value := range values {
		cache[week] = value
	}
	return nil
}

func readPayrollScaleWeeks(ctx context.Context, tx pgx.Tx, weeks []string) (map[string]payrollScaleWeek, error) {
	rows, err := tx.Query(ctx, wmtPayrollScaleSQL, weeks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]payrollScaleWeek{}
	for _, week := range weeks {
		values[week] = payrollScaleWeek{{}, {}}
	}
	for rows.Next() {
		var week, id, ticket, date, original, due, driver, truck, payer string
		var collected, ticketWeek bool
		if err = rows.Scan(&week, &id, &ticket, &date, &original, &due, &driver, &truck, &payer, &collected, &ticketWeek); err != nil {
			return nil, err
		}
		charge := PayAutoCharge{
			Name: "CAT Scale · " + date + " · " + ticket, Source: payrollScaleSourcePrefix + id,
			Amount: due, ScaleDue: due, ScaleOriginalAmount: original,
			scaleCollected: collected, scaleTicketWeek: ticketWeek,
		}
		value := values[week]
		if payer == "driver" {
			value[0][driver] = append(value[0][driver], charge)
		} else {
			value[1][truck] = append(value[1][truck], charge)
		}
	}
	return values, rows.Err()
}

const wmtReportChargeRowsSQL = `jsonb_array_elements(CASE WHEN jsonb_typeof(f.report->'autoCharges')='array'
 THEN f.report->'autoCharges' ELSE '[]'::jsonb END) c`

// Every period shares the ticket's dated payer and the statement that last
// collected it. Keep the edge through reopening to publish both destinations.
const payrollScaleEdgesSQL = `SELECT 'driver:'||t.driver_id,'truck:'||t.truck_id
 FROM weighmytruck_transactions t WHERE t.driver_id IS NOT NULL AND t.truck_id IS NOT NULL
 UNION SELECT 'driver:'||payroll_driver_at(t.driver_id,t.transaction_date),'truck:'||t.truck_id
 FROM weighmytruck_transactions t WHERE t.driver_id IS NOT NULL AND t.truck_id IS NOT NULL
 UNION SELECT CASE f.kind WHEN 'driver' THEN 'driver:' ELSE 'truck:' END||f.destination,'truck:'||t.truck_id
 FROM (` + wmtStatementReportsSQL + `) f CROSS JOIN LATERAL ` + wmtReportChargeRowsSQL + `
 JOIN weighmytruck_transactions t ON c->>'source'='weighmytruck:'||t.id
 WHERE t.truck_id IS NOT NULL`

// Recheck exact source amounts and all-week collection under the settlement's
// financial locks before accepting a reviewed scale charge.
func validatePayrollScaleCollection(ctx context.Context, tx pgx.Tx, charges []PayAutoCharge) error {
	var ids, amounts, dues, originals []string
	for _, charge := range charges {
		if id, ok := strings.CutPrefix(charge.Source, payrollScaleSourcePrefix); ok {
			ids = append(ids, id)
			amounts = append(amounts, charge.Amount)
			dues = append(dues, charge.ScaleDue)
			originals = append(originals, charge.ScaleOriginalAmount)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `WITH scale_paid AS MATERIALIZED (`+wmtCollectionsSQL+`)
 SELECT count(DISTINCT t.id)=cardinality($1::uuid[])
 FROM unnest($1::uuid[],$2::text[],$3::text[],$4::text[]) reviewed(id,amount,due,original)
 JOIN weighmytruck_transactions t ON t.id=reviewed.id`+wmtChargeJoins+wmtBalanceJoinSQL+`
 WHERE -t.amount=nullif(reviewed.original,'')::numeric AND balance.due=nullif(reviewed.due,'')::numeric
 AND (paid.source IS NULL OR balance.due<>0)
 AND reviewed.amount::numeric BETWEEN least(0,balance.due) AND greatest(0,balance.due)`,
		ids, amounts, dues, originals).Scan(&valid)
	if err == nil && !valid {
		return ErrDriverPayConflict
	}
	return err
}
