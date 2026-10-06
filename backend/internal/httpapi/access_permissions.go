package httpapi

import (
	"net/http"
	"slices"
	"strings"
)

// Unknown resource families deny access even to administrators until explicitly
// mapped. This middleware also covers child routers and direct API requests.
func routePermission(method, path string) string {
	path = strings.TrimSuffix(path, "/")
	if path == "/auth/session" || path == "/auth/logout" || path == "/auth/password" {
		return "authenticated"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	resource := parts[0]
	read := method == http.MethodGet || method == http.MethodHead
	pair := func(area string) string {
		if read {
			return area + ".read"
		}
		return area + ".write"
	}
	if resource == "settings" {
		return "access.manage"
	}
	if resource == "jobs" {
		switch path {
		case "/jobs/sync-loads":
			return "loads.sync"
		case "/jobs/sync-fuel":
			return "fuel.sync"
		case "/jobs/sync-tolls":
			return "tolls.sync"
		case "/jobs/sync-eld":
			return "driver_board.write"
		}
		return ""
	}
	if resource == "drivers" && (strings.HasSuffix(path, "/pay-history") || strings.HasSuffix(path, "/settlement-history")) {
		return "payroll.read"
	}
	switch resource {
	case "drivers", "trucks", "dispatchers", "updaters", "investors", "driver-directory", "driver-intake", "files", "irp-files", "cdl-files":
		return pair("fleet")
	case "loads":
		if read {
			return "loads.read"
		}
	case "driver-board":
		return pair("driver_board")
	case "gross-board":
		return pair("board")
	case "fuel-transactions", "fuel-dashboard":
		if read {
			return "fuel.read"
		}
	case "tolls", "toll-dashboard":
		if read {
			return "tolls.read"
		}
	case "expenses":
		return "authenticated"
	case "expense-settings":
		return "expense_settings.manage"
	case "driver-pay", "investor-pay":
		if strings.HasSuffix(path, "/finalize") || strings.HasSuffix(path, "/reopen") {
			return "payroll.finalize"
		}
		return pair("payroll")
	case "driver-charges", "truck-charges":
		return pair("charges")
	case "tasks":
		return pair("tasks")
	case "financial-dashboard":
		if read {
			return "reports.read"
		}
	}
	return ""
}

func requirePermission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := authSessionFromContext(r.Context())
		permission := routePermission(r.Method, r.URL.Path)
		if !ok || permission == "" || (permission != "authenticated" && !slices.Contains(session.User.Permissions, permission)) {
			writeAPIError(w, http.StatusForbidden, "you do not have permission to perform this action")
			return
		}
		next.ServeHTTP(w, r)
	})
}
