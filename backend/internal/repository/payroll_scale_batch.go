package repository

import (
	"context"

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

// The dated payer query reads the complete week, independent of the selected
// component. Batch it after all canonical writes, including frozen-destination
// checks, and retain only within this exact transaction's publication phase.
func preparePayrollScaleWeeks(ctx context.Context, tx pgx.Tx, weeks []string) error {
	cache := payrollScaleCache(ctx, tx)
	if cache == nil || len(weeks) < 2 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT date_trunc('week',t.transaction_date)::date::text,t.id::text,t.ticket_number,t.transaction_date::text,t.amount::text,
 coalesce(pd.driver_id::text,''),coalesce(t.truck_id::text,''),b.payer`+wmtChargeFrom+`
 WHERE date_trunc('week',t.transaction_date)::date=ANY($1::date[]) AND t.driver_id IS NOT NULL
 AND t.match_warning NOT LIKE '%conflicts%' AND NOT frozen.misplaced
 AND (b.payer='driver' OR (b.payer='investor' AND s.owner_id=i.id)) ORDER BY t.transaction_date,t.ticket_number`, weeks)
	if err != nil {
		return err
	}
	defer rows.Close()
	values := map[string]payrollScaleWeek{}
	for _, week := range weeks {
		values[week] = payrollScaleWeek{{}, {}}
	}
	for rows.Next() {
		var week, id, ticket, date, amount, driver, truck, payer string
		if err = rows.Scan(&week, &id, &ticket, &date, &amount, &driver, &truck, &payer); err != nil {
			return err
		}
		n, err := chargeCents(amount)
		if err != nil {
			return err
		}
		charge := PayAutoCharge{Name: "CAT Scale · " + date + " · " + ticket, Amount: chargeMoney(-n), Source: "weighmytruck:" + id}
		value := values[week]
		if payer == "driver" {
			value[0][driver] = append(value[0][driver], charge)
		} else {
			value[1][truck] = append(value[1][truck], charge)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for week, value := range values {
		cache[week] = value
	}
	return nil
}
