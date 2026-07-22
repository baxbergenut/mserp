package repository

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// formatPersonName returns the canonical form used to store and display a
// person's name. Matching uses normalizeName so casing and extra whitespace
// from upstream systems do not create duplicate people.
func formatPersonName(value string) string {
	personNameCaser := cases.Title(language.Und)
	words := strings.Fields(value)
	for wordIndex, word := range words {
		parts := strings.Split(strings.ToLower(word), "'")
		for partIndex, part := range parts {
			parts[partIndex] = personNameCaser.String(part)
		}
		words[wordIndex] = strings.Join(parts, "'")
	}
	return strings.Join(words, " ")
}

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// personNameTokenSignature matches high-confidence upstream name permutations
// such as "Jane Mary Doe" and "Doe Jane Mary" without treating partial names
// as the same person. Punctuation and token order are ignored; every token must
// still be present with the same multiplicity.
func personNameTokenSignature(value string) string {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)
	tokens := strings.Fields(normalized)
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

// normalizeTruckUnit keeps truck identifiers visually consistent and makes
// case-insensitive DataTruck values resolve to the same fleet record.
func normalizeTruckUnit(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(value), " "))
}

func formatPersonNamePtr(value *string) *string {
	if value == nil {
		return nil
	}
	formatted := formatPersonName(*value)
	if formatted == "" {
		return nil
	}
	return &formatted
}

func normalizeTruckUnitPtr(value *string) *string {
	if value == nil {
		return nil
	}
	formatted := normalizeTruckUnit(*value)
	if formatted == "" {
		return nil
	}
	return &formatted
}
