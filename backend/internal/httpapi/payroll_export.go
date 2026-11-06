package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"
	"mserp/internal/repository"
)

const payrollXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func registerPayrollExport(r chi.Router, path, kind string, repo *repository.DriverPayRepository, fail func(http.ResponseWriter, error)) {
	r.Get(path+"/export", func(w http.ResponseWriter, r *http.Request) {
		week, err := availablePayrollWeek(r.URL.Query().Get("weekStart"), repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		query, err := payPageFilters(r, kind)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		query.PageSize = 2147483647
		if query.ID != "" && !isUUID(query.ID) {
			writeAPIError(w, http.StatusBadRequest, "Invalid statement")
			return
		}
		var report repository.DriverPayWeek
		if repo.PreparedReadsEnabled() {
			report, err = repo.PreparedPayroll(r.Context(), kind, week, query, false)
		} else {
			report, err = repo.PayrollPage(r.Context(), kind, week, query)
		}
		if err != nil {
			fail(w, err)
			return
		}
		book, err := payrollStatementWorkbook(kind, report.Drivers)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", payrollXLSX)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-pay-%s.xlsx"`, kind, report.WeekStart))
		_, _ = w.Write(book.Bytes())
	})
}

func payrollStatementWorkbook(kind string, statements []repository.DriverPayDriver) (*bytes.Buffer, error) {
	book := excelize.NewFile()
	defer book.Close()
	const sheet = "Statements"
	if err := book.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	headers := []string{"Driver Name", "Total Salary Payable", "Reimbursed Amount", "Accessorials"}
	if err := book.SetSheetRow(sheet, "A1", &headers); err != nil {
		return nil, err
	}
	header, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	if err := book.SetCellStyle(sheet, "A1", "D1", header); err != nil {
		return nil, err
	}
	currencyFormat := `"$"#,##0.00;-"$"#,##0.00`
	currency, err := book.NewStyle(&excelize.Style{CustomNumFmt: &currencyFormat})
	if err != nil {
		return nil, err
	}
	if err := book.SetColStyle(sheet, "B:D", currency); err != nil {
		return nil, err
	}
	if err := book.SetColWidth(sheet, "A", "A", 48); err != nil {
		return nil, err
	}
	if err := book.SetColWidth(sheet, "B", "D", 24); err != nil {
		return nil, err
	}
	type exportRow struct {
		name                              string
		payable, reimbursed, accessorials int64
	}
	rows := make([]exportRow, 0, len(statements))
	for _, statement := range statements {
		payable := statement.NetPayCents()
		if payable <= 0 {
			continue
		}
		name := statement.FullName
		if kind == "investor" {
			names := make([]string, 0, len(statement.OperatingDrivers))
			for _, driver := range statement.OperatingDrivers {
				names = append(names, driver.Name)
			}
			if len(names) == 0 {
				// Older frozen statements identified operators in the earnings rows.
				for _, charge := range statement.AutoCharges {
					if strings.HasPrefix(charge.Source, "driver:") {
						names = append(names, strings.TrimPrefix(charge.Name, "Driver earnings · "))
					}
				}
			}
			name = strings.Join(names, " / ")
		}
		rows = append(rows, exportRow{name, payable, statement.ReimbursedCents(), statement.AccessorialCents()})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].name) < strings.ToLower(rows[j].name)
	})
	for i, entry := range rows {
		row := []any{entry.name, float64(entry.payable) / 100, float64(entry.reimbursed) / 100, float64(entry.accessorials) / 100}
		if err := book.SetSheetRow(sheet, fmt.Sprintf("A%d", i+2), &row); err != nil {
			return nil, err
		}
	}
	return book.WriteToBuffer()
}
