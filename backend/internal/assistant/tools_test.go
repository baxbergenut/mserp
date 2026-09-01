package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mserp/internal/repository"
)

func TestResolveDateRangeMondaySunday(t *testing.T) {
	executor := &ToolExecutor{now: func() time.Time {
		return time.Date(2026, time.September, 1, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	}}
	result, err := executor.resolveDateRange(json.RawMessage(`{"period":"last_week"}`))
	if err != nil {
		t.Fatal(err)
	}
	value := result.Data.(map[string]string)
	if value["dateFrom"] != "2026-08-24" || value["dateTo"] != "2026-08-30" {
		t.Fatalf("unexpected last week: %#v", value)
	}
	result, err = executor.resolveDateRange(json.RawMessage(`{"period":"this_week"}`))
	if err != nil {
		t.Fatal(err)
	}
	value = result.Data.(map[string]string)
	if value["dateFrom"] != "2026-08-31" || value["dateTo"] != "2026-09-01" {
		t.Fatalf("unexpected this week: %#v", value)
	}
}

func TestParseDatesRejectsInvalidRanges(t *testing.T) {
	if _, _, err := parseDates("2026-09-02", "2026-09-01", false); err == nil {
		t.Fatal("expected reversed range error")
	}
	if _, _, err := parseDates("", "", true); err != nil {
		t.Fatalf("optional blank range: %v", err)
	}
	if _, _, err := parseDates("09/01/2026", "09/02/2026", false); err == nil {
		t.Fatal("expected date format error")
	}
}

func TestHighRiskChanges(t *testing.T) {
	cases := []struct {
		entity, key string
		want        bool
	}{{"driver", "payRate", true}, {"driver", "active", true}, {"dispatcher", "payPercentage", true}, {"truck", "active", true}, {"truck", "status", false}, {"driver", "phone", false}}
	for _, test := range cases {
		if got := hasHighRiskChange(test.entity, map[string]any{test.key: "value"}); got != test.want {
			t.Errorf("%s.%s high risk=%v want %v", test.entity, test.key, got, test.want)
		}
	}
}

func TestDriverPatchPreservesOmittedFields(t *testing.T) {
	phone := "555-1000"
	input := repository.DriverInput{FullName: "John Smith", PayType: "cpm", PayRate: 0.65, Phone: &phone, Active: true}
	if err := applyDriverChanges(&input, map[string]any{"notes": "Updated"}); err != nil {
		t.Fatal(err)
	}
	if input.Phone == nil || *input.Phone != "555-1000" || input.PayRate != 0.65 || input.Notes == nil || *input.Notes != "Updated" {
		t.Fatalf("patch overwrote omitted values: %#v", input)
	}
}

func TestCSVAttachmentIncludesAllRows(t *testing.T) {
	attachment, err := csvAttachment([]map[string]any{{"date": "2026-08-24", "amount": "10.25"}, {"date": "2026-08-25", "amount": "20.50"}}, "report.csv", "report")
	if err != nil {
		t.Fatal(err)
	}
	if attachment == nil || !strings.Contains(string(attachment.Data), "2026-08-24") || !strings.Contains(string(attachment.Data), "20.50") {
		t.Fatalf("unexpected CSV: %#v", attachment)
	}
}

func TestCSVAttachmentEscapesSpreadsheetFormula(t *testing.T) {
	attachment, err := csvAttachment([]map[string]any{{"merchant": "=HYPERLINK(\"bad\")"}}, "report.csv", "report")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(attachment.Data), "'=HYPERLINK") {
		t.Fatalf("formula was not neutralized: %s", attachment.Data)
	}
}
