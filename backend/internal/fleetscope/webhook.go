// Package fleetscope defines signed driver intake and the separate outbound fleet protocol.
package fleetscope

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mserp/internal/phone"
)

const MaxBodyBytes = 64 << 10

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Options struct {
	CompanyID string
	Secret    string
	Sync      SyncOptions
}

func (o Options) Validate() error {
	if o.CompanyID == "" && o.Secret == "" {
		return nil
	}
	if !uuidPattern.MatchString(o.CompanyID) || len(o.Secret) < 32 {
		return errors.New("FLEETSCOPE_COMPANY_ID must be a UUID and FLEETSCOPE_WEBHOOK_SECRET must contain at least 32 bytes; set both or neither")
	}
	return nil
}

type Driver struct {
	ID             string `json:"id"`
	FullName       string `json:"fullName"`
	FirstName      string `json:"firstName,omitempty"`
	LastName       string `json:"lastName,omitempty"`
	Email          string `json:"email,omitempty"`
	Phone          string `json:"phone,omitempty"`
	DriverType     string `json:"driverType"`
	HireDate       string `json:"hireDate"`
	Address        string `json:"address,omitempty"`
	City           string `json:"city,omitempty"`
	State          string `json:"state,omitempty"`
	PostalCode     string `json:"postalCode,omitempty"`
	LicenseNumber  string `json:"licenseNumber,omitempty"`
	LicenseState   string `json:"licenseState,omitempty"`
	LicenseExpires string `json:"licenseExpires,omitempty"`
}

type Applicant struct {
	ID            string `json:"id"`
	FullName      string `json:"fullName"`
	FirstName     string `json:"firstName,omitempty"`
	LastName      string `json:"lastName,omitempty"`
	Status        string `json:"status"`
	HiredDriverID string `json:"hiredDriverId,omitempty"`
}

type Event struct {
	Applicant       *Applicant `json:"applicant,omitempty"`
	Version         int        `json:"version"`
	EventID         string     `json:"eventId"`
	Type            string     `json:"type"`
	CompanyID       string     `json:"companyId"`
	OccurredAt      time.Time  `json:"occurredAt"`
	Driver          Driver     `json:"driver"`
	TerminationDate string     `json:"terminationDate,omitempty"`
}

func (e *Event) Validate() error {
	if e.Version != 1 || (e.Type != "driver.hired" && e.Type != "driver.terminated" && e.Type != "applicant.updated") || !uuidPattern.MatchString(e.EventID) ||
		!uuidPattern.MatchString(e.CompanyID) || e.OccurredAt.IsZero() {
		return errors.New("invalid event envelope")
	}
	e.CompanyID = strings.ToLower(e.CompanyID)
	e.EventID = strings.ToLower(e.EventID)
	e.Driver.ID = strings.ToLower(e.Driver.ID)
	for field, max := range map[*string]int{
		&e.Driver.FullName: 200, &e.Driver.Email: 254, &e.Driver.Phone: 50,
		&e.Driver.Address: 500, &e.Driver.City: 100, &e.Driver.State: 2,
		&e.Driver.PostalCode: 20, &e.Driver.LicenseNumber: 100, &e.Driver.LicenseState: 2,
	} {
		*field = strings.TrimSpace(*field)
		if len(*field) > max || strings.ContainsAny(*field, "\x00\r\n") {
			return errors.New("invalid driver field")
		}
	}
	if e.Type == "applicant.updated" {
		a := e.Applicant
		if a == nil || !uuidPattern.MatchString(a.ID) || e.Driver != (Driver{}) || e.TerminationDate != "" {
			return errors.New("applicant.updated requires an applicant object only")
		}
		a.ID = strings.ToLower(a.ID)
		a.HiredDriverID = strings.ToLower(a.HiredDriverID)
		a.FullName = strings.TrimSpace(a.FullName)
		if err := validateNameParts(&a.FirstName, &a.LastName); err != nil {
			return err
		}
		if len(a.FullName) == 0 || len(a.FullName) > 200 || strings.ContainsAny(a.FullName, "\x00\r\n") {
			return errors.New("invalid applicant fullName")
		}
		switch a.Status {
		case "applied", "rejected", "withdrawn":
			if a.HiredDriverID != "" {
				return errors.New("hiredDriverId requires hired status")
			}
		case "hired":
			if !uuidPattern.MatchString(a.HiredDriverID) {
				return errors.New("hired status requires hiredDriverId")
			}
		default:
			return errors.New("invalid applicant status")
		}
		if e.OccurredAt.After(time.Now().Add(5 * time.Minute)) {
			return errors.New("occurredAt cannot be in the future")
		}
		return nil
	}
	if e.Applicant != nil || !uuidPattern.MatchString(e.Driver.ID) {
		return errors.New("driver lifecycle requires a driver object only")
	}
	if err := validateNameParts(&e.Driver.FirstName, &e.Driver.LastName); err != nil {
		return err
	}
	if e.Type == "driver.terminated" {
		if e.Driver.FullName == "" {
			return errors.New("fullName is required")
		}
		if _, err := time.Parse(time.DateOnly, e.TerminationDate); err != nil {
			return errors.New("terminationDate must use YYYY-MM-DD")
		}
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			return err
		}
		if e.TerminationDate > time.Now().In(loc).Format(time.DateOnly) {
			return errors.New("terminationDate cannot be in the future; send when termination takes effect")
		}
		// Termination is a minimal identity notification, not a profile update.
		if e.Driver != (Driver{ID: e.Driver.ID, FullName: e.Driver.FullName, FirstName: e.Driver.FirstName, LastName: e.Driver.LastName}) {
			return errors.New("terminated driver must contain only id and name fields")
		}
		return nil
	}
	if e.TerminationDate != "" {
		return errors.New("terminationDate is only valid for driver.terminated")
	}
	if e.Driver.FullName == "" || (e.Driver.DriverType != "company" && e.Driver.DriverType != "owner_operator") {
		return errors.New("fullName and a valid driverType are required")
	}
	if e.Driver.Email != "" {
		address, err := mail.ParseAddress(e.Driver.Email)
		if err != nil || address.Address != e.Driver.Email {
			return errors.New("invalid driver email")
		}
	}
	if _, err := phone.Normalize(e.Driver.Phone); err != nil {
		return err
	}
	if _, err := time.Parse(time.DateOnly, e.Driver.HireDate); err != nil {
		return errors.New("hireDate must use YYYY-MM-DD")
	}
	if e.Driver.LicenseExpires != "" {
		if _, err := time.Parse(time.DateOnly, e.Driver.LicenseExpires); err != nil {
			return errors.New("licenseExpires must use YYYY-MM-DD")
		}
	}
	return nil
}

// Legacy immutable deliveries omit both parts; never infer a split from fullName.
func validateNameParts(first, last *string) error {
	if *first == "" && *last == "" {
		return nil
	}
	for _, part := range []*string{first, last} {
		if !utf8.ValidString(*part) || strings.ContainsAny(*part, "\x00\r\n") {
			return errors.New("invalid name part")
		}
		*part = strings.TrimSpace(*part)
		if *part == "" || utf8.RuneCountInString(*part) > 50 {
			return errors.New("firstName and lastName must both contain 1–50 characters")
		}
	}
	return nil
}

// Sign signs the exact bytes delivered, with a fresh Unix-seconds timestamp on
// every attempt. Event IDs and payload bytes remain unchanged on retries.
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func Verify(secret, timestamp, signature string, body []byte, now time.Time) bool {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp || seconds < now.Unix()-300 || seconds > now.Unix()+300 {
		return false
	}
	if !strings.HasPrefix(signature, "v1=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "v1="))
	if err != nil {
		return false
	}
	expected, _ := hex.DecodeString(strings.TrimPrefix(Sign(secret, timestamp, body), "v1="))
	return hmac.Equal(provided, expected)
}
