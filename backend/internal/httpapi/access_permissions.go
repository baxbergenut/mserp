package httpapi

import (
	"mserp/internal/repository"
	"net/http"
	"strings"
)

type routeAccess struct{ method, path, permission string }

// Every registered authenticated route has an explicit policy, including handler-owned checks.
var routeAccessPolicies = []routeAccess{
	{"GET", "/expenses/{id}/external-settlements", "authenticated"},
	{"POST", "/expenses/{id}/external-settlements", "expenses.settle"},
	{"GET", "/feedback", "authenticated"},
	{"POST", "/feedback", "authenticated"},
	{"GET", "/feedback/tags", "authenticated"},
	{"GET", "/feedback/messages", "authenticated"},
	{"GET", "/feedback/messages/count", "authenticated"},
	{"PUT", "/feedback/messages/{id}/read", "authenticated"},
	{"GET", "/feedback/{id}", "authenticated"},
	{"PUT", "/feedback/{id}", "authenticated"},
	{"PUT", "/feedback/{id}/vote", "authenticated"},
	{"POST", "/feedback/{id}/comments", "authenticated"},
	{"GET", "/feedback/attachments/{id}", "authenticated"},
	{"GET", "/weekly-pti", "pti.read"},
	{"GET", "/weekly-pti/matrix", "pti.read"},
	{"PUT", "/weekly-pti/layout", "pti.edit"},
	{"POST", "/weekly-pti/weeks", "pti.edit"},
	{"PATCH", "/weekly-pti/{week}/{truck}", "pti.edit"},
	{"GET", "/weekly-pti/{week}/{truck}/history", "pti.read"},
	{"GET", "/fleet-status/catalog", "authenticated"},
	{"GET", "/settings/status-colors", "authenticated"},
	{"PUT", "/settings/status-colors", "access.manage"},
	{"GET", "/drivers/{id}/status-history", "drivers.read"},
	{"PATCH", "/drivers/{id}/availability", "drivers.edit"},
	{"GET", "/trucks/{id}/status-history", "trucks.read"},
	{"PATCH", "/trucks/{id}/condition", "trucks.edit"},
	{"PATCH", "/trucks/{id}/status", "trucks.edit"},
	{"GET", "/inventory", "inventory.read"},
	{"GET", "/inventory/history", "inventory.read"},
	{"PUT", "/inventory/{collection}/{id}", "inventory.manage"},
	{"POST", "/inventory/movements", "inventory.manage"},
	{"GET", "/inventory/imports", "inventory.import"},
	{"POST", "/inventory/imports/preview", "inventory.import"},
	{"PUT", "/inventory/imports/{id}", "inventory.import"},
	{"POST", "/inventory/imports/{id}/commit", "inventory.import"},
	{"GET", "/trailers", "trailers.read"},
	{"GET", "/trailers/{id}", "trailers.read"},
	{"PUT", "/trailers/{id}", "trailers.manage"},
	{"GET", "/trailers/{id}/history", "trailers.read"},
	{"GET", "/gross-board/events", "board.read"},
	{"GET", "/gross-board/presence", "board.read"},
	{"PUT", "/gross-board/presence", "board.read"},
	{"DELETE", "/gross-board/presence/{clientId}", "board.read"},
	{"GET", "/driver-board/events", "driver_board.read"},
	{"GET", "/driver-board/presence", "driver_board.read"},
	{"PUT", "/driver-board/presence", "driver_board.read"},
	{"DELETE", "/driver-board/presence/{clientId}", "driver_board.read"},
	{"POST", "/fleetscope-sync/assignments", "fleetscope.reconcile"},
	{"POST", "/fleetscope-sync/operations/{id}/supersede", "fleetscope.reconcile"},
	{"POST", "/auth/logout", "authenticated"},
	{"POST", "/auth/password", "authenticated"},
	{"GET", "/auth/session", "authenticated"},
	{"PUT", "/auth/theme", "authenticated"},
	{"POST", "/auth/birthday", "authenticated"},
	{"GET", "/dispatcher-pay", "dispatcher_pay.read"},
	{"GET", "/dispatcher-pay/rules", "dispatcher_pay.read"},
	{"POST", "/dispatcher-pay/rules", "dispatcher_pay.rules"},
	{"PUT", "/dispatcher-pay/inputs", "dispatcher_pay.edit"},
	{"POST", "/dispatcher-pay/finalize", "dispatcher_pay.finalize"},
	{"POST", "/dispatcher-pay/reopen", "dispatcher_pay.reopen"},
	{"GET", "/dispatchers", "dispatchers.read"},
	{"POST", "/dispatchers", "dispatchers.create"},
	{"DELETE", "/dispatchers/{id}", "dispatchers.delete"},
	{"GET", "/dispatchers/{id}", "dispatchers.read"},
	{"PUT", "/dispatchers/{id}", "dispatchers.edit"},
	{"GET", "/driver-board", "driver_board.read"},
	{"PUT", "/driver-board", "driver_board.write"},
	{"POST", "/driver-board/order", "driver_board.write"},
	{"POST", "/driver-board/dispatcher-order", "driver_board.write"},
	{"GET", "/driver-board/history", "driver_board.read"},
	{"POST", "/driver-board/history/{id}/undo", "driver_board.write"},
	{"GET", "/driver-board/loads/{driverId}", "driver_board.read"},
	{"POST", "/driver-board/loads/{driverId}", "driver_board.write"},
	{"GET", "/driver-charges", "charges.read"},
	{"POST", "/driver-charges/bulk", "charges.edit"},
	{"POST", "/driver-charges/confirm", "charges.edit"},
	{"PUT", "/driver-charges/recurring", "charges.edit"},
	{"POST", "/driver-charges/reopen", "charges.edit"},
	{"POST", "/driver-charges/schedules", "charges.create"},
	{"GET", "/driver-charges/schedules/{id}/history", "charges.read"},
	{"GET", "/driver-charges/schedules/{id}/preview", "charges.read"},
	{"POST", "/driver-charges/schedules/preview", "charges.read"},
	{"POST", "/driver-charges/types", "authenticated"},
	{"DELETE", "/driver-charges/types/{id}", "charges.delete"},
	{"GET", "/driver-directory", "drivers.read"},
	{"GET", "/driver-intake", "drivers.private.read"},
	{"GET", "/driver-intake/{id}", "drivers.private.read"},
	{"POST", "/driver-intake/{id}/complete", "drivers.create"},
	{"GET", "/driver-pay", "driver_pay.read"},
	{"PUT", "/driver-pay", "driver_pay.edit"},
	{"POST", "/driver-pay/accept-system", "driver_pay.edit"},
	{"POST", "/driver-pay/transfer-load", "driver_pay.edit"},
	{"POST", "/driver-pay/finalize", "driver_pay.finalize"},
	{"POST", "/driver-pay/refresh-loads", "driver_pay.edit"},
	{"POST", "/driver-pay/reopen", "driver_pay.reopen"},
	{"POST", "/driver-pay/undo-system", "driver_pay.edit"},
	{"GET", "/driver-services/{id}", "drivers.read"},
	{"POST", "/driver-services/{id}", "drivers.read"},
	{"GET", "/drivers", "drivers.read"},
	{"POST", "/drivers", "drivers.create"},
	{"DELETE", "/drivers/{id}", "drivers.delete"},
	{"GET", "/drivers/{id}", "drivers.read"},
	{"PUT", "/drivers/{id}", "drivers.edit"},
	{"GET", "/drivers/{id}/assignments", "drivers.read"},
	{"GET", "/drivers/{id}/location", "drivers.read"},
	{"GET", "/drivers/{id}/notes", "drivers.read"},
	{"POST", "/drivers/{id}/notes", "drivers.edit"},
	{"GET", "/drivers/{id}/pay-history", "driver_pay.read"},
	{"GET", "/drivers/{id}/settlement-history", "driver_pay.read"},
	{"PATCH", "/drivers/{id}/status", "drivers.edit"},
	{"GET", "/drivers/{id}/team", "drivers.read"},
	{"PUT", "/drivers/{id}/team", "driver_pay.edit"},
	{"GET", "/drivers/setup-defaults", "escrow.read"},
	{"GET", "/drivers/team-trucks", "drivers.read"},
	{"GET", "/escrows", "escrow.read"},
	{"PUT", "/escrows/{id}/opening", "escrow.edit"},
	{"POST", "/escrows/{id}/releases", "escrow.create"},
	{"PUT", "/escrows/{id}/releases/{releaseID}", "escrow.read"},
	{"GET", "/escrows/settings", "escrow.read"},
	{"PUT", "/escrows/settings", "escrow.edit"},
	{"GET", "/expense-settings", "expense_settings.manage"},
	{"POST", "/expense-settings", "expense_settings.manage"},
	{"PUT", "/expense-settings/{id}", "expense_settings.manage"},
	{"GET", "/expenses", "authenticated"},
	{"GET", "/expenses/form-choices", "authenticated"},
	{"POST", "/expenses", "authenticated"},
	{"DELETE", "/expenses/{id}", "authenticated"},
	{"GET", "/expenses/{id}", "authenticated"},
	{"GET", "/expenses/{id}/documents", "authenticated"},
	{"POST", "/expenses/{id}/documents", "authenticated"},
	{"GET", "/expenses/documents/{id}", "authenticated"},
	{"PUT", "/expenses/{id}", "authenticated"},
	{"POST", "/expenses/{id}/unverify", "authenticated"},
	{"POST", "/expenses/{id}/verify", "authenticated"},
	{"POST", "/expenses/bulk", "authenticated"},
	{"POST", "/expenses/extract", "authenticated"},
	{"GET", "/files/{id}", "authenticated"},
	{"GET", "/financial-dashboard", "reports.read"},
	{"GET", "/dashboard/summary", "authenticated"},
	{"GET", "/dashboard/loads", "board.read"},
	{"GET", "/dashboard/cost-review", "authenticated"},
	{"GET", "/fleetscope-sync", "fleetscope.read"},
	{"POST", "/fleetscope-sync/activate", "fleetscope.reconcile"},
	{"POST", "/fleetscope-sync/baseline", "fleetscope.reconcile"},
	{"POST", "/fleetscope-sync/operations/{id}/resolve", "fleetscope.reconcile"},
	{"POST", "/fleetscope-sync/operations/{id}/retry", "fleetscope.manage"},
	{"POST", "/fleetscope-sync/pause", "fleetscope.manage"},
	{"POST", "/fleetscope-sync/resume", "fleetscope.reconcile"},
	{"POST", "/fleetscope-sync/review", "fleetscope.reconcile"},
	{"POST", "/fleetscope-sync/snapshot", "fleetscope.reconcile"},
	{"GET", "/fuel-dashboard", "fuel.read"},
	{"GET", "/fuel-transactions", "fuel.read"},
	{"GET", "/gross-board", "board.read"},
	{"PUT", "/gross-board", "board.write"},
	{"GET", "/gross-board/balance", "board.read"},
	{"GET", "/gross-board/loads", "board.read"},
	{"GET", "/investor-pay", "investor_pay.read"},
	{"PUT", "/investor-pay", "investor_pay.edit"},
	{"POST", "/investor-pay/accept-system", "investor_pay.edit"},
	{"POST", "/investor-pay/transfer-load", "investor_pay.edit"},
	{"POST", "/investor-pay/finalize", "investor_pay.finalize"},
	{"GET", "/investor-pay/history", "investor_pay.read"},
	{"POST", "/investor-pay/reopen", "investor_pay.reopen"},
	{"POST", "/investor-pay/undo-system", "investor_pay.edit"},
	{"GET", "/investors", "investors.read"},
	{"POST", "/investors", "investors.create"},
	{"GET", "/investors/{id}", "investors.read"},
	{"PUT", "/investors/{id}", "investors.edit"},
	{"POST", "/irp-files", "trucks.documents.edit"},
	{"POST", "/jobs/sync-eld", "driver_board.write"},
	{"POST", "/jobs/sync-fuel", "fuel.sync"},
	{"POST", "/jobs/sync-loads", "loads.sync"},
	{"POST", "/jobs/sync-tolls", "tolls.sync"},
	{"GET", "/loads", "loads.read"},
	{"GET", "/lookups/dispatchers", "authenticated"},
	{"GET", "/lookups/drivers", "authenticated"},
	{"GET", "/lookups/investors", "authenticated"},
	{"GET", "/lookups/trucks", "authenticated"},
	{"GET", "/lookups/updaters", "authenticated"},
	{"GET", "/maintenance", "authenticated"},
	{"POST", "/maintenance", "authenticated"},
	{"GET", "/maintenance/{id}", "authenticated"},
	{"PUT", "/maintenance/{id}", "authenticated"},
	{"POST", "/maintenance/{id}/documents", "authenticated"},
	{"GET", "/maintenance/{id}/history", "authenticated"},
	{"POST", "/maintenance/{id}/void", "authenticated"},
	{"GET", "/maintenance/access", "authenticated"},
	{"GET", "/maintenance/programs", "authenticated"},
	{"GET", "/maintenance/program-workspace", "authenticated"},
	{"POST", "/maintenance/programs", "service_programs.manage"},
	{"PUT", "/maintenance/programs/{id}", "service_programs.manage"},
	{"GET", "/maintenance/programs/{id}/trucks", "authenticated"},
	{"POST", "/maintenance/programs/{id}/trucks", "authenticated"},
	{"PUT", "/maintenance/programs/{id}/trucks/{truckId}/baseline", "authenticated"},
	{"GET", "/maintenance/program-trucks", "authenticated"},
	{"GET", "/maintenance/program-assignees", "service_programs.manage"},
	{"GET", "/maintenance/documents/{id}", "authenticated"},
	{"GET", "/maintenance/due", "authenticated"},
	{"POST", "/maintenance/extract", "authenticated"},
	{"GET", "/maintenance/odometer", "authenticated"},
	{"GET", "/maintenance/odometer-sources/{truckId}", "authenticated"},
	{"PUT", "/maintenance/odometer-sources/{truckId}", "authenticated"},
	{"GET", "/maintenance/options", "authenticated"},
	{"GET", "/maintenance/preventative", "authenticated"},
	{"PUT", "/maintenance/preventative/{id}", "authenticated"},
	{"GET", "/maintenance/readings", "authenticated"},
	{"POST", "/maintenance/readings", "authenticated"},
	{"PUT", "/maintenance/readings/{id}", "authenticated"},
	{"GET", "/maintenance/reports", "authenticated"},
	{"POST", "/maintenance/schedules", "authenticated"},
	{"PUT", "/maintenance/schedules/{id}", "authenticated"},
	{"PUT", "/maintenance/types/{code}", "authenticated"},
	{"POST", "/maintenance/vendors", "authenticated"},
	{"PUT", "/maintenance/vendors/{id}", "authenticated"},
	{"GET", "/prepass-fleet", "tolls.read"},
	{"POST", "/prepass-fleet/change", "authenticated"},
	{"POST", "/prepass-fleet/link", "tolls.link"},
	{"POST", "/prepass-fleet/link/preview", "tolls.link"},
	{"POST", "/prepass-fleet/operations/{id}/verify", "tolls.fleet.recover"},
	{"POST", "/prepass-fleet/refresh", "tolls.fleet.sync"},
	{"GET", "/relay-drivers", "fuel.read"},
	{"POST", "/relay-drivers/{id}/link", "tasks.review"},
	{"POST", "/relay-drivers/bulk", "authenticated"},
	{"POST", "/relay-drivers/change", "authenticated"},
	{"POST", "/relay-drivers/operations/{id}/verify", "fuel.drivers.recover"},
	{"POST", "/relay-drivers/refresh", "fuel.drivers.sync"},
	{"GET", "/settings/access", "access.manage"},
	{"GET", "/settings/driver-pay", "access.manage"},
	{"PUT", "/settings/driver-pay", "access.manage"},
	{"POST", "/settings/roles", "access.manage"},
	{"PUT", "/settings/roles/{id}", "access.manage"},
	{"GET", "/settings/system-tasks", "access.manage"},
	{"PUT", "/settings/system-tasks/{kind}", "access.manage"},
	{"POST", "/settings/users", "access.manage"},
	{"PUT", "/settings/users/{id}", "access.manage"},
	{"POST", "/settings/users/{id}/revoke", "access.manage"},
	{"GET", "/sidebar-counts", "authenticated"},
	{"GET", "/tasks", "tasks.read"},
	{"GET", "/tasks/count", "tasks.read"},
	{"GET", "/tasks/custom", "tasks.read"},
	{"POST", "/tasks/custom", "tasks.create"},
	{"DELETE", "/tasks/custom/{id}", "tasks.delete"},
	{"PATCH", "/tasks/custom/{id}", "tasks.edit"},
	{"PUT", "/tasks/custom/{id}", "tasks.edit"},
	{"GET", "/tasks/escrow/{id}", "tasks.read"},
	{"POST", "/tasks/escrow/{id}/complete", "tasks.review"},
	{"GET", "/tasks/events", "tasks.read"},
	{"GET", "/tasks/five-eld/{id}", "tasks.read"},
	{"POST", "/tasks/five-eld/{id}/complete", "tasks.review"},
	{"POST", "/tasks/offboarding/{id}/confirm", "tasks.review"},
	{"GET", "/tasks/relay-identities", "tasks.read"},
	{"POST", "/tasks/relay-identities/{id}/review", "tasks.review"},
	{"GET", "/tasks/users", "tasks.read"},
	{"GET", "/toll-dashboard", "tolls.read"},
	{"GET", "/tolls", "tolls.read"},
	{"GET", "/tolls/truck-links", "tolls.read"},
	{"GET", "/tolls/attention", "tolls.read"},
	{"GET", "/tolls/truck-link-options", "tolls.read"},
	{"POST", "/tolls/truck-links", "tolls.link"},
	{"POST", "/tolls/truck-links/{id}/undo", "tolls.link"},
	{"POST", "/tolls/truck-links/preview", "tolls.link"},
	{"POST", "/tolls/{id}/review", "tolls.link"},
	{"POST", "/tolls/reviews/bulk", "tolls.link"},
	{"GET", "/truck-charges", "charges.read"},
	{"PUT", "/truck-charges/recurring", "charges.edit"},
	{"PUT", "/truck-charges/recurring/bulk", "charges.edit"},
	{"PUT", "/truck-charges/terms", "investor_terms.edit"},
	{"GET", "/trucks", "trucks.read"},
	{"POST", "/trucks", "trucks.create"},
	{"DELETE", "/trucks/{id}", "trucks.delete"},
	{"GET", "/trucks/{id}", "trucks.read"},
	{"GET", "/trucks/{id}/datatruck", "trucks.read"},
	{"POST", "/trucks/{id}/datatruck", "trucks.read"},
	{"PUT", "/trucks/{id}", "trucks.edit"},
	{"GET", "/trucks/{id}/assignments", "trucks.read"},
	{"GET", "/trucks/{id}/location", "trucks.read"},
	{"GET", "/trucks/{id}/notes", "trucks.read"},
	{"POST", "/trucks/{id}/notes", "trucks.edit"},
	{"GET", "/updaters", "updaters.read"},
	{"POST", "/updaters", "updaters.create"},
	{"DELETE", "/updaters/{id}", "updaters.delete"},
	{"PUT", "/updaters/{id}", "updaters.edit"},
	{"GET", "/weighmytruck", "weighmytruck.read"},
	{"POST", "/weighmytruck/{id}/link", "weighmytruck.link"},
	{"POST", "/weighmytruck/{id}/verify", "weighmytruck.recover"},
	{"PUT", "/weighmytruck/billing", "driver_pay.edit"},
	{"POST", "/weighmytruck/change", "authenticated"},
	{"POST", "/weighmytruck/sync", "weighmytruck.sync"},
	{"GET", "/weighmytruck/transactions", "weighmytruck.read"},
	{"PUT", "/weighmytruck/transactions/{id}/link", "driver_pay.edit"},
}

func routePermission(method, path string) string {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	path = strings.TrimSuffix(path, "/")
	actual := strings.Split(path, "/")
	bestPermission, bestScore := "", -1
	for _, policy := range routeAccessPolicies {
		if method != policy.method {
			continue
		}
		expected := strings.Split(policy.path, "/")
		if len(actual) != len(expected) {
			continue
		}
		match := true
		score := 0
		for i, p := range expected {
			if strings.HasPrefix(p, "{") && actual[i] != "" {
				continue
			}
			score++
			if p != actual[i] {
				match = false
				break
			}
		}
		if match && score > bestScore {
			bestPermission, bestScore = policy.permission, score
		}
	}
	return bestPermission
}
func permissionDenied(w http.ResponseWriter) {
	writeJSON(w, 403, map[string]string{"error": "You do not have permission to perform this action", "code": "permission_denied"})
}
func requireCapabilities(w http.ResponseWriter, r *http.Request, keys ...string) bool {
	s, ok := authSessionFromContext(r.Context())
	if !ok {
		permissionDenied(w)
		return false
	}
	for _, key := range keys {
		if !repository.HasPermission(s.User, key) {
			permissionDenied(w)
			return false
		}
	}
	return true
}
func requirePermission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permission := routePermission(r.Method, r.URL.Path)
		if permission == "" || (permission != "authenticated" && !requireCapabilities(w, r, permission)) {
			if permission == "" {
				permissionDenied(w)
			}
			return
		}
		next.ServeHTTP(&permissionResponseWriter{ResponseWriter: w, request: r}, r)
	})
}
