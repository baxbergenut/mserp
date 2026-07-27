package repository

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
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
	tokens := personNameTokens(value)
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

// relayDriverNameMatchQuality returns a confidence score for the name
// variations Relay commonly sends. It accepts token reordering, omitted middle
// names, suffixes, and a single small spelling difference, while leaving
// partial or otherwise ambiguous matches for contact evidence to resolve.
func relayDriverNameMatchQuality(left, right string) int {
	leftTokens := meaningfulPersonNameTokens(left)
	rightTokens := meaningfulPersonNameTokens(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}

	leftSet := tokenSet(leftTokens)
	rightSet := tokenSet(rightTokens)
	if setsEqual(leftSet, rightSet) {
		return 100
	}

	shorter, longer := leftSet, rightSet
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if len(shorter) >= 2 && isTokenSubset(shorter, longer) {
		return 90
	}

	exactShared := sharedTokenCount(leftSet, rightSet)
	if len(leftSet) == len(rightSet) && exactShared+1 == len(leftSet) && exactShared >= 1 {
		var unmatchedLeft, unmatchedRight string
		for token := range leftSet {
			if _, ok := rightSet[token]; !ok {
				unmatchedLeft = token
			}
		}
		for token := range rightSet {
			if _, ok := leftSet[token]; !ok {
				unmatchedRight = token
			}
		}
		if similarNameToken(unmatchedLeft, unmatchedRight) {
			return 80
		}
	}

	if exactShared > 0 {
		return min(exactShared*20, 60)
	}
	return 0
}

func personNameTokens(value string) []string {
	decomposed := norm.NFD.String(value)
	normalized := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, decomposed)
	return strings.Fields(normalized)
}

func meaningfulPersonNameTokens(value string) []string {
	tokens := personNameTokens(value)
	meaningful := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if len([]rune(token)) == 1 || isNameSuffix(token) || token == "owner" || isNumericToken(token) {
			continue
		}
		meaningful = append(meaningful, token)
	}
	return meaningful
}

func isNameSuffix(token string) bool {
	switch token {
	case "jr", "junior", "sr", "senior", "ii", "iii", "iv", "v":
		return true
	default:
		return false
	}
}

func isNumericToken(token string) bool {
	for _, r := range token {
		if !unicode.IsNumber(r) {
			return false
		}
	}
	return token != ""
}

func tokenSet(tokens []string) map[string]struct{} {
	result := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		result[token] = struct{}{}
	}
	return result
}

func setsEqual(left, right map[string]struct{}) bool {
	return len(left) == len(right) && isTokenSubset(left, right)
}

func isTokenSubset(shorter, longer map[string]struct{}) bool {
	for token := range shorter {
		if _, ok := longer[token]; !ok {
			return false
		}
	}
	return true
}

func sharedTokenCount(left, right map[string]struct{}) int {
	count := 0
	for token := range left {
		if _, ok := right[token]; ok {
			count++
		}
	}
	return count
}

func similarNameToken(left, right string) bool {
	leftLength := len([]rune(left))
	rightLength := len([]rune(right))
	if leftLength < 5 || rightLength < 5 {
		return false
	}
	distance := levenshteinDistance(left, right)
	longerLength := max(leftLength, rightLength)
	return distance <= 2 && distance*4 <= longerLength
}

func levenshteinDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	previous := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range leftRunes {
		current := make([]int, len(rightRunes)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range rightRunes {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous = current
	}
	return previous[len(rightRunes)]
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
