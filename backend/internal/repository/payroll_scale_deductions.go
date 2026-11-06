package repository

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

const payrollScaleSourcePrefix = "weighmytruck:"

// An omitted map preserves the saved choices for clients that don't edit
// scale deductions. The full unpaid amount resets a choice to automatic.
func reviewScaleDeductions(card DriverPayDriver, input map[string]string) (map[string]string, error) {
	if input == nil {
		input = card.Edits.ScaleDeductions
	}
	choices := map[string]string{}
	if len(input) == 0 {
		return choices, nil
	}
	due := map[string]string{}
	for _, rows := range [][]PayAutoCharge{card.AutoCharges, card.DeferredScaleCharges} {
		for _, row := range rows {
			if strings.HasPrefix(row.Source, payrollScaleSourcePrefix) {
				due[row.Source] = row.ScaleDue
				if row.ScaleDue == "" {
					due[row.Source] = row.Amount
				}
			}
		}
	}
	for source, amount := range input {
		full, visible := due[source]
		if !visible {
			if saved, exists := card.Edits.ScaleDeductions[source]; !exists || saved != amount {
				return nil, ErrDriverPayConflict
			}
			choices[source] = amount
			continue
		}
		limit, err := chargeCents(full)
		if err != nil {
			return nil, err
		}
		if err = validatePaySourceAmount("CAT Scale deduction", amount, limit); err != nil {
			return nil, err
		}
		n, _ := chargeCents(amount)
		if n != limit || n == 0 {
			choices[source] = chargeMoney(n)
		}
	}
	return choices, nil
}

// Frozen AutoCharges are the collection receipts. Keep deferred ticket
// details separate so a zero can never be mistaken for a collection.
func applyScaleDeductions(card *DriverPayDriver) {
	if len(card.Edits.ScaleDeductions) == 0 && len(card.DeferredScaleCharges) == 0 && !slices.ContainsFunc(card.AutoCharges, func(row PayAutoCharge) bool {
		return row.ScaleDue != "" && row.Amount != row.ScaleDue
	}) {
		return
	}
	charges := make([]PayAutoCharge, 0, len(card.AutoCharges)+len(card.DeferredScaleCharges))
	var deferred []PayAutoCharge
	for _, rows := range [][]PayAutoCharge{card.AutoCharges, card.DeferredScaleCharges} {
		for _, row := range rows {
			choice, chosen := card.Edits.ScaleDeductions[row.Source]
			if row.ScaleDue != "" {
				row.Amount = row.ScaleDue
			}
			if chosen && choice == "0.00" {
				deferred = append(deferred, row)
			} else {
				if chosen {
					row.Amount = choice
				}
				charges = append(charges, row)
			}
		}
	}
	card.AutoCharges, card.DeferredScaleCharges = charges, deferred
}

func readDriverScaleDeductions(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	cards := map[string]*DriverPayDriver{}
	var ids []string
	for i := range report.Drivers {
		card := &report.Drivers[i]
		if card.Edits.Version > 0 && (card.Settlement == nil || !card.Settlement.Finalized) {
			ids = append(ids, card.ID)
			cards[card.ID] = card
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT driver_id::text,scale_deductions FROM driver_pay_weeks WHERE week_start=$1::date AND driver_id=ANY($2::uuid[])`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var choices map[string]string
		if err = rows.Scan(&id, &choices); err != nil {
			return err
		}
		card := cards[id]
		card.Edits.ScaleDeductions = choices
	}
	return rows.Err()
}
