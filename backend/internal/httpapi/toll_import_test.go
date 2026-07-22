package httpapi

import (
	"strings"
	"testing"
)

const validTollHeader = "PostingDate,InvoiceDate,CustID,Source,ReadType,PPTagID,ETagID_Plate,EquipID,Agency,Entry_Plaza,Entry_Date,Entry_Time,Exit_Plaza,Exit_Date,Exit_Time,Toll_Class,Miles,Toll_Amount\n"

func TestParseTollCSV(t *testing.T) {
	csv := validTollHeader +
		"07/13/26,2026-07-31,365108,EZPass,Transponder,777686194,01605532349,097,WVPEDTA,,,,Chelyan,07/10/26,19:14:14,8,,13.00\n" +
		"07/18/26,2026-07-31,365108,EZPass,Plate,,ABC123,52413,NYSTA,40,07/17/26,12:00:00,41,07/17/26,12:30:00,5,12.5,(7.36)\n"

	rows, total, err := parseTollCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parseTollCSV() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if total != 564 {
		t.Errorf("total = %d cents, want 564", total)
	}
	if rows[0].EquipmentUnit != "097" {
		t.Errorf("equipment unit = %q, want leading zero preserved", rows[0].EquipmentUnit)
	}
	if rows[1].AmountCents != -736 {
		t.Errorf("adjustment amount = %d cents, want -736", rows[1].AmountCents)
	}
	if rows[0].EntryDate != nil || rows[0].EntryTime != nil {
		t.Error("blank entry date/time should remain nil")
	}
}

func TestParseTollCSVUsesOccurrenceForIdenticalRows(t *testing.T) {
	row := "07/13/26,2026-07-31,365108,EZPass,Transponder,777686194,01605532349,097,WVPEDTA,,,,Chelyan,07/10/26,19:14:14,8,,13.00\n"
	rows, _, err := parseTollCSV(strings.NewReader(validTollHeader + row + row))
	if err != nil {
		t.Fatalf("parseTollCSV() error = %v", err)
	}
	if rows[0].Fingerprint == rows[1].Fingerprint {
		t.Error("identical source rows should receive distinct occurrence fingerprints")
	}
}

func TestParseTollCSVRejectsUnexpectedHeader(t *testing.T) {
	_, _, err := parseTollCSV(strings.NewReader(strings.Replace(validTollHeader, "EquipID", "Unit", 1)))
	if err == nil || !strings.Contains(err.Error(), "expected \"EquipID\"") {
		t.Fatalf("error = %v, want actionable header error", err)
	}
}

func TestParseTollCSVRejectsPartialEntryTimestamp(t *testing.T) {
	csv := validTollHeader +
		"07/13/26,2026-07-31,365108,EZPass,Transponder,777686194,01605532349,097,WVPEDTA,Entry,07/10/26,,Chelyan,07/10/26,19:14:14,8,,13.00\n"
	_, _, err := parseTollCSV(strings.NewReader(csv))
	if err == nil || !strings.Contains(err.Error(), "must either both be present") {
		t.Fatalf("error = %v, want paired entry timestamp error", err)
	}
}
