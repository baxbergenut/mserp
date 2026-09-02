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

func TestReadOnlyPromptCannotAccessActionTools(t *testing.T) {
	for _, definition := range ToolDefinitionsForPrompt("how much gross did he do last week?") {
		if isActionTool(definition.Function.Name) {
			t.Fatalf("read-only prompt exposed action tool %s", definition.Function.Name)
		}
	}
	definitions := ToolDefinitionsForPrompt("please sync loads")
	foundSync := false
	for _, definition := range definitions {
		if definition.Function.Name == "sync_data" {
			foundSync = true
		}
		if definition.Function.Name != "sync_data" && isActionTool(definition.Function.Name) {
			t.Fatalf("sync prompt exposed unrelated action tool %s", definition.Function.Name)
		}
	}
	if !foundSync {
		t.Fatal("explicit sync prompt did not expose sync_data")
	}
}

func TestModelToolContentRejectsOversizedResult(t *testing.T) {
	content := modelToolContent(map[string]string{"records": strings.Repeat("x", 9000)}, true)
	if len(content) > 1000 || !strings.Contains(string(content), "safe model payload limit") || !strings.Contains(string(content), "completeCSVReady") {
		t.Fatalf("unexpected oversized fallback: %s", content)
	}
}

func TestBoundedModelMessagesCompactsEarlierToolResults(t *testing.T) {
	messages := []groq.AssistantMessage{
		{Role: "system", Content: "system"},
		{Role: "assistant", ToolCalls: []groq.AssistantToolCall{{ID: "first"}}},
		{Role: "tool", ToolCallID: "first", Content: strings.Repeat("a", 12000)},
		{Role: "assistant", ToolCalls: []groq.AssistantToolCall{{ID: "latest"}}},
		{Role: "tool", ToolCallID: "latest", Content: strings.Repeat("b", 7000)},
	}
	bounded := boundedModelMessages(messages)
	encoded, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 18<<10 {
		t.Fatalf("bounded messages are still too large: %d", len(encoded))
	}
	if !strings.Contains(bounded[2].Content, "omitted") || bounded[4].Content != messages[4].Content {
		t.Fatalf("wrong tool result was compacted: %#v", bounded)
	}
}

func TestFleetListResultIsBounded(t *testing.T) {
	matches := make([]map[string]any, 20)
	for index := range matches {
		matches[index] = map[string]any{"id": strings.Repeat("a", 36), "fullName": "Driver Name"}
	}
	result := fleetListResult("driver", 175, matches)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 4096 || result["count"] != 175 || result["returned"] != 20 || result["truncated"] != true {
		t.Fatalf("unexpected bounded fleet result: size=%d result=%#v", len(encoded), result)
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

func TestCompactFuelModelDataKeepsCompleteCountAndSmallPreview(t *testing.T) {
	rows := make([]repository.FuelTransaction, 100)
	for index := range rows {
		rows[index] = repository.FuelTransaction{
			DriverName: "Driver Name", MerchantName: "Merchant", LocationName: "Location",
			PurchasedAt:     time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC),
			TotalAmountPaid: 100.25, FuelAmount: 95.25, FuelVolume: 25.5, DEFAmount: 5,
		}
	}
	freshness := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
	data := compactFuelModelData(rows, "2026-08-24", "2026-08-30", &freshness)
	if data["transactionCount"] != 100 || data["previewCount"] != 10 {
		t.Fatalf("unexpected compact report counts: %#v", data)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 8000 {
		t.Fatalf("compact model payload is too large: %d bytes", len(encoded))
	}
	totals := data["totals"].(map[string]string)
	if totals["totalCharged"] != "10025.00" || totals["fuelGallons"] != "2550.000" {
		t.Fatalf("unexpected totals: %#v", totals)
	}
}

func TestTrimConversationHistoryUsesCharacterBudget(t *testing.T) {
	history := make([]groq.AssistantMessage, 10)
	for index := range history {
		history[index] = groq.AssistantMessage{Role: "assistant", Content: strings.Repeat("x", 2000)}
	}
	trimmed := trimConversationHistory(history)
	if len(trimmed) != 3 {
		t.Fatalf("trimmed history length = %d, want 3", len(trimmed))
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
