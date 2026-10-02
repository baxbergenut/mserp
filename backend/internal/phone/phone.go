// Package phone defines the canonical optional US phone number used by MSERP.
package phone

import (
	"errors"
	"strings"
)

var ErrInvalid = errors.New("phone must contain exactly 10 digits (an optional +1 country code is accepted)")

// Normalize accepts common display punctuation and an optional US country code.
// It never truncates, pads, guesses digits, or combines multiple phone numbers.
func Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	var digits strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case strings.ContainsRune("+(). -", r):
		default:
			return "", ErrInvalid
		}
	}
	result := digits.String()
	if len(result) == 11 && result[0] == '1' {
		result = result[1:]
	}
	if len(result) != 10 {
		return "", ErrInvalid
	}
	return result, nil
}

// Imported returns only valid contact evidence. Raw integration payloads retain
// upstream values for review; malformed contacts must not block transaction sync.
func Imported(value string) *string {
	normalized, err := Normalize(value)
	if err != nil || normalized == "" {
		return nil
	}
	return &normalized
}

// Search lets users find canonical contacts using the UI's display format.
func Search(value string) string {
	if normalized := Imported(value); normalized != nil {
		return *normalized
	}
	return strings.TrimSpace(value)
}
