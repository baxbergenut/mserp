package httpapi

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
	"mserp/internal/repository"
)

func TestPayrollStatementWorkbook(t *testing.T) {
	statements := []repository.DriverPayDriver{
		{FullName: "Zoe", OperatingDrivers: []repository.PayPerson{{Name: "Zoe"}}, Loads: []repository.DriverPayLoad{{Fee: "500.00"}}, SalaryCarry: "100.00",
			Edits: repository.DriverPayEdits{
				Adjustments: []repository.DriverPayAdjustment{{Kind: "reimbursement", Amount: "10.00"}},
				ExpenseDeductions: []repository.ExpenseDeduction{
					{Source: "reimburse", Amount: "25.25"}, {Source: "reimburse", Amount: "12.50"}, {Source: "reimburse", Amount: "0.00"}, {Amount: "40.00"}, {Source: "accessorial", Amount: "50.00"},
				},
			}},
		{FullName: "alice", OperatingDrivers: []repository.PayPerson{{Name: "alice"}}, Loads: []repository.DriverPayLoad{{Fee: "81.99"}},
			AutoCharges: []repository.PayAutoCharge{{Source: "expense-reimburse:old", Amount: "1.01"}}},
		{FullName: "No payout", Loads: []repository.DriverPayLoad{{Fee: "25.00"}}, SalaryCarry: "25.00"},
		{FullName: "Shortfall", Loads: []repository.DriverPayLoad{{Fee: "25.00"}}, SalaryCarry: "50.00"},
	}
	for _, kind := range []string{"driver", "investor"} {
		t.Run(kind, func(t *testing.T) {
			body, err := payrollStatementWorkbook(kind, statements)
			if err != nil {
				t.Fatal(err)
			}
			book, err := excelize.OpenReader(bytes.NewReader(body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			defer book.Close()
			rows, err := book.GetRows("Statements")
			want := [][]string{{"Driver Name", "Total Salary Payable", "Reimbursed Amount", "Accessorials"}, {"alice", "$83.00", "$1.01", "$0.00"}, {"Zoe", "$457.75", "$37.75", "$50.00"}}
			if err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("got %v, want %v: %v", rows, want, err)
			}
			for _, cell := range []string{"B2", "C2", "D2", "B3", "C3", "D3"} {
				kind, err := book.GetCellType("Statements", cell)
				if err != nil || (kind != excelize.CellTypeNumber && kind != excelize.CellTypeUnset) {
					t.Fatalf("%s is not numeric: %v %v", cell, kind, err)
				}
			}
		})
	}
}
