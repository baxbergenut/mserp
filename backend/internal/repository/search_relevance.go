package repository

import "strings"

// searchRelevanceOrder prefixes the usual ordering when a caller requests
// relevance. Fields are repository SQL expressions and $1 is the search term.
func searchRelevanceOrder(enabled bool, fields ...string) string {
	if !enabled {
		return ""
	}
	ranks := make([]string, 0, len(fields))
	for _, field := range fields {
		value := "lower(" + field + ")"
		ranks = append(ranks, "CASE WHEN "+value+" = lower($1) THEN 0"+
			" WHEN starts_with("+value+", lower($1)) THEN 1"+
			" WHEN strpos("+value+", lower($1)) > 0 THEN 2 ELSE 3 END")
	}
	return "LEAST(" + strings.Join(ranks, ", ") + "), "
}
