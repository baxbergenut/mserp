package assistant

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"mserp/internal/groq"
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

func TestToolDefinitionsNeverEncodeNullRequired(t *testing.T) {
	definitions := ToolDefinitions()
	encoded, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"required":null`) {
		t.Fatalf("tool schema contains invalid null required array: %s", encoded)
	}
	for _, definition := range definitions {
		required, exists := definition.Function.Parameters["required"]
		if exists && required == nil {
			t.Fatalf("tool %s has a nil required value", definition.Function.Name)
		}
	}
}

func TestFleetSearchMatchesFirstAndLastAroundMiddleName(t *testing.T) {
	if !fleetSearchMatches("Roderick Nunn", "Roderick Earl Nunn") {
		t.Fatal("expected first and last name to match stored middle name")
	}
	if !fleetSearchMatches("Rod Nun", "Roderick Earl Nunn") {
		t.Fatal("expected partial name tokens to match")
	}
	if fleetSearchMatches("Roderick Smith", "Roderick Earl Nunn") {
		t.Fatal("unexpected mismatched last name")
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

func TestRetryDelayHonorsGroqCooldownAndBackoff(t *testing.T) {
	rateErr := &groq.RateLimitError{Message: "limited", RetryAfter: 18 * time.Second}
	if got := retryDelay(rateErr, 1); got != 20*time.Second {
		t.Fatalf("rate-limit retry delay = %v, want 20s", got)
	}
	if got := retryDelay(errors.New("temporary"), 4); got != 40*time.Second {
		t.Fatalf("fourth retry delay = %v, want 40s", got)
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
