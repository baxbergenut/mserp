package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"mserp/internal/phone"
	"mserp/internal/repository"
)

type investorRequest struct {
	FullName string  `json:"fullName"`
	DriverID *string `json:"driverId"`
	Email    string  `json:"email"`
	Phone    string  `json:"phone"`
	Notes    string  `json:"notes"`
	Active   bool    `json:"active"`
}

func (v investorRequest) validate() (repository.InvestorInput, error) {
	v.FullName = strings.TrimSpace(v.FullName)
	if (v.DriverID == nil && v.FullName == "") || utf8.RuneCountInString(v.FullName) > 200 {
		return repository.InvestorInput{}, errors.New("name is required and must be at most 200 characters")
	}
	if err := validateOptionalUUID(v.DriverID, "driver id"); err != nil {
		return repository.InvestorInput{}, err
	}
	normalizedPhone, err := phone.Normalize(v.Phone)
	if err != nil {
		return repository.InvestorInput{}, err
	}
	if utf8.RuneCountInString(v.Notes) > 5000 || len(v.Email) > 254 {
		return repository.InvestorInput{}, errors.New("contact details or notes are too long")
	}
	return repository.InvestorInput{FullName: v.FullName, DriverID: v.DriverID, Email: optionalString(v.Email), Phone: optionalString(normalizedPhone), Notes: optionalString(v.Notes), Active: v.Active}, nil
}
func (h fleetHandler) listInvestors(w http.ResponseWriter, r *http.Request) {
	values, err := h.repo.ListInvestors(r.Context(), strings.TrimSpace(r.URL.Query().Get("search")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if wantsPagination(r) {
		// Keep owner identities in unpaginated lookups. The investor directory
		// shows driver-linked owners only when they own an additional truck.
		values = investorDirectory(values, strings.EqualFold(r.URL.Query().Get("includeCompany"), "true"))
		p, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		p = p.Normalize(len(values))
		start := p.Offset()
		end := min(start+p.PageSize, len(values))
		writeJSON(w, 200, repository.NewPage(values[start:end], len(values), p))
		return
	}
	writeJSON(w, 200, values)
}
func investorDirectory(values []repository.Investor, includeCompany bool) []repository.Investor {
	filtered := make([]repository.Investor, 0, len(values))
	for _, investor := range values {
		if investor.IsCompany && !includeCompany || investor.DriverID != nil && len(investor.Trucks) < 2 {
			continue
		}
		filtered = append(filtered, investor)
	}
	return filtered
}
func (h fleetHandler) saveInvestor(w http.ResponseWriter, r *http.Request) {
	id := ""
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		var ok bool
		id, ok = pathID(w, r)
		if !ok {
			return
		}
		status = http.StatusOK
	}
	var request investorRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	v, err := h.repo.SaveInvestor(r.Context(), id, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, status, v)
}
