package httpapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
	"net/http"
	"strings"
)

// Lookup grants expose operational identities for an authorized workflow, never
// full profiles, compensation, private fields, notes or document metadata.
func registerLookupRoutes(r chi.Router, repo *repository.FleetRepository) {
	for _, kind := range []string{"drivers", "trucks", "dispatchers", "updaters", "investors"} {
		r.Get("/lookups/"+kind, func(w http.ResponseWriter, r *http.Request) {
			s, _ := authSessionFromContext(r.Context())
			allowed := false
			for _, p := range lookupGrants[kind] {
				if repository.HasPermission(s.User, p) {
					allowed = true
					break
				}
			}
			if !allowed {
				permissionDenied(w)
				return
			}
			var rows any
			var err error
			switch kind {
			case "drivers":
				rows, err = repo.DriverOptions(r.Context())
			case "trucks":
				rows, err = repo.TruckOptions(r.Context())
			case "dispatchers":
				rows, err = repo.ListDispatchers(r.Context())
			case "updaters":
				rows, err = repo.ListUpdaters(r.Context())
			case "investors":
				rows, err = repo.ListInvestors(r.Context(), "")
			}
			if err != nil {
				writeAPIError(w, 500, "Could not load choices")
				return
			}
			raw, _ := json.Marshal(rows)
			var records []map[string]json.RawMessage
			if json.Unmarshal(raw, &records) != nil {
				writeAPIError(w, 500, "Could not load choices")
				return
			}
			fields := strings.Fields("id fullName unitNumber status operatingStatus active employmentType driverType isOwnerOperator driverId driverName truckId truckUnit teamRole teamTruckId teamTruckUnit teamPartnerId teamPartnerName teamStartWeek teamVersion dispatcherId dispatcherName ownerId ownerName ownerSince ownershipVersion isCompanyOwned isCompany trucks year make model shift extension mainUpdaterId afterHoursUpdaterId driverCount")
			if kind == "drivers" && repository.HasPermission(s.User, "drivers.read") {
				fields = append(fields, "phone", "email")
			}
			out := make([]map[string]json.RawMessage, 0, len(records))
			for _, record := range records {
				item := map[string]json.RawMessage{}
				for _, key := range fields {
					if value, ok := record[key]; ok {
						item[key] = value
					}
				}
				out = append(out, item)
			}
			writeJSON(w, 200, out)
		})
	}
}

// Each choice list is limited to pages that actually consume it. A grant to an
// unrelated directory does not make every fleet identity searchable.
var lookupGrants = map[string][]string{
	"drivers":     {"drivers.read", "trucks.read", "dispatchers.read", "investors.read", "charges.read", "escrow.read", "driver_pay.read", "board.read", "driver_board.read", "tasks.read", "fuel.read", "tolls.read", "weighmytruck.read", "expenses.read"},
	"trucks":      {"trucks.read", "drivers.read", "investors.read", "charges.read", "driver_pay.read", "investor_pay.read", "board.read", "driver_board.read", "tasks.read", "tolls.read", "weighmytruck.read", "expenses.read"},
	"dispatchers": {"dispatchers.read", "drivers.read", "board.read", "driver_board.read"},
	"updaters":    {"updaters.read", "dispatchers.read", "driver_board.read"},
	"investors":   {"investors.read", "trucks.read", "drivers.read", "charges.read", "investor_pay.read", "tasks.read", "expenses.read"},
}
