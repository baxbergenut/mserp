package repository

import (
	"regexp"
	"strings"
)

const expenseTextSearchSQL = `($1 = '' OR concat_ws(' ', e.category, (SELECT name FROM expense_settings WHERE id=e.category_id), COALESCE(t.unit_number, e.unit_number),
		COALESCE(d.full_name, e.driver_name), e.payment_type, e.expense_type,
		e.reference_number, e.description, e.covered_by, e.paid_by, e.broker_load_number)
	ILIKE '%' || $1 || '%')`

var expenseSearchAmountPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)?(?:\.[0-9]{1,2})?$`)

// normalizeExpenseSearchAmount recognizes a complete invoice total, not a
// substring, range or float. Keep the decimal string exact for PostgreSQL numeric.
// Unrecognized inputs retain ordinary text-search behavior.
func normalizeExpenseSearchAmount(search string) string {
	value := strings.TrimSpace(search)
	value = strings.TrimSpace(strings.TrimPrefix(value, "$"))
	if len(value) == 0 || len(value) > 64 || !expenseSearchAmountPattern.MatchString(value) {
		return ""
	}
	value = strings.ReplaceAll(value, ",", "")
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign = "-"
	}
	value = strings.TrimLeft(value, "+-")
	if value == "" {
		return ""
	}
	whole, fraction, _ := strings.Cut(value, ".")
	whole = strings.TrimLeft(whole, "0")
	if len(whole) > 12 { // expenses.amount is numeric(14,2).
		return ""
	}
	if whole == "" {
		whole = "0"
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	if whole == "0" && fraction == "00" {
		sign = ""
	}
	return sign + whole + "." + fraction
}
