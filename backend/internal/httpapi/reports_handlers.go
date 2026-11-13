package httpapi

import (
	"context"
	"encoding/csv"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type reportsSource interface {
	Read(context.Context, repository.ReportsQuery) (repository.ReportsResult, error)
}

func registerReportsRoutes(r chi.Router, logger *slog.Logger, reports reportsSource) {
	read := func(export bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			view := chi.URLParam(r, "view")
			permission := map[string]string{
				repository.ReportCompany: "reports.read", repository.ReportOperator: "reports.read",
				repository.ReportDispatcher: "reports.performance", repository.ReportDriver: "reports.performance",
			}[view]
			if permission == "" {
				writeAPIError(w, 404, "Unknown report")
				return
			}
			if !requireCapabilities(w, r, permission) {
				return
			}
			period, err := repository.ResolveReportsPeriod(q.Get("from"), q.Get("to"), q.Get("compareFrom"), q.Get("compareTo"), time.Now())
			if err != nil {
				writeAPIError(w, 400, err.Error())
				return
			}
			filter := repository.ReportsQuery{View: view, Period: period, Truck: q.Get("truck"), Driver: q.Get("driver"), Dispatcher: q.Get("dispatcher")}
			if (filter.Truck != "" && !isUUID(filter.Truck)) || (filter.Driver != "" && !isUUID(filter.Driver)) ||
				(filter.Dispatcher != "" && !isUUID(filter.Dispatcher) &&
					(!strings.HasPrefix(filter.Dispatcher, "historical:") || len(filter.Dispatcher) > 512 || filter.Dispatcher == "historical:")) {
				writeAPIError(w, 400, "Choose valid report filters")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
			defer cancel()
			result, err := reports.Read(ctx, filter)
			if err != nil {
				logger.Error("read report", "view", view, "error", err)
				writeAPIError(w, 500, "Could not load the report")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			if export {
				w.Header().Set("Content-Type", "text/csv; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="`+view+"-"+period.DateFrom+`.csv"`)
				if err = writeReportsCSV(csv.NewWriter(w), view, result); err != nil {
					logger.Warn("write report CSV", "view", view, "error", err)
				}
				return
			}
			writeJSON(w, 200, result)
		}
	}
	r.Get("/reports/{view}", read(false))
	r.Get("/reports/{view}/export.csv", read(true))
}

func reportCSVText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") ||
		(trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + value
	}
	return value
}

var reportCSVMoney = map[string]bool{
	"gross": true, "provisionalGross": true, "grossPerTruckWeek": true, "loadedRPM": true,
	"revenuePerMile": true, "costPerMile": true, "earnedPay": true, "fuel": true, "tolls": true, "scales": true,
	"maintenance": true, "equipment": true, "carrierCosts": true, "carrierFees": true, "contribution": true,
	"settlementEarnings": true, "reimbursements": true, "deductions": true, "escrowReturned": true,
	"escrowApplied": true, "escrowWithheld": true, "netPayable": true, "unassignedCost": true, "amount": true,
}

var reportCSVColumns = map[string][]string{
	repository.ReportCompany:    {"gross", "earnedPay", "fuel", "tolls", "scales", "maintenance", "equipment", "contribution", "margin", "miles", "revenuePerMile", "costPerMile", "provisionalGross", "unassignedCost"},
	repository.ReportOperator:   {"gross", "carrierFees", "carrierCosts", "contribution", "provisionalGross", "unassignedCost", "settlementEarnings", "reimbursements", "deductions", "escrowWithheld", "escrowReturned", "escrowApplied", "netPayable"},
	repository.ReportDispatcher: {"completedLoads", "gross", "truckWeeks", "grossPerTruckWeek", "loadedRPM", "deadheadPct", "provisionalLoads"},
	repository.ReportDriver:     {"completedLoads", "activeDays", "miles", "pickupOnTime", "pickupCoverage", "deliveryOnTime", "deliveryCoverage", "fuelEfficiency", "provisionalLoads"},
}

func writeReportsCSV(w *csv.Writer, view string, result repository.ReportsResult) error {
	columns := map[string]bool{}
	for _, key := range reportCSVColumns[view] {
		columns[key] = true
	}
	for _, period := range [][]repository.ReportRow{result.Current, result.Comparison} {
		for _, row := range period {
			for key := range row.Metrics {
				columns[key] = true
			}
			for _, source := range row.Sources {
				for key := range source.Values {
					columns[key] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(columns))
	for key := range columns {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	header := []string{"Period", "From", "Through", "Group", "Record type", "Source", "Source ID", "Record", "Date", "Truck", "Driver", "Dispatcher", "Status", "Note", "Pickup actual", "Pickup scheduled", "Delivery actual", "Delivery scheduled"}
	for _, key := range keys {
		label := key
		if reportCSVMoney[key] {
			label += " (USD)"
		}
		header = append(header, label)
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, period := range []struct {
		label, from, to string
		rows            []repository.ReportRow
	}{
		{"Current", result.Period.DateFrom, result.Period.DateTo, result.Current},
		{"Comparison", result.Period.CompareFrom, result.Period.CompareTo, result.Comparison},
	} {
		for _, row := range period.rows {
			base := []string{period.label, period.from, period.to, reportCSVText(row.Name)}
			if err := writeReportCSVRow(w, append(append([]string{}, base...), "Summary", "", "", "", "", "", "", "", "", "", "", "", "", ""), keys, row.Metrics); err != nil {
				return err
			}
			for _, source := range row.Sources {
				fields := []string{"Source", source.Kind, source.ID, source.Label, source.Date, source.Truck, source.Driver, source.Dispatcher, source.State, source.Note,
					source.PickupActual, source.PickupScheduled, source.DeliveryActual, source.DeliveryScheduled}
				for i, value := range fields {
					fields[i] = reportCSVText(value)
				}
				if err := writeReportCSVRow(w, append(append([]string{}, base...), fields...), keys, source.Values); err != nil {
					return err
				}
			}
		}
	}
	w.Flush()
	return w.Error()
}

func writeReportCSVRow(w *csv.Writer, fields, keys []string, values map[string]float64) error {
	for _, key := range keys {
		cell := ""
		if value, ok := values[key]; ok {
			if reportCSVMoney[key] {
				value /= 100
			}
			cell = strconv.FormatFloat(value, 'f', -1, 64)
		}
		fields = append(fields, cell)
	}
	return w.Write(fields)
}
