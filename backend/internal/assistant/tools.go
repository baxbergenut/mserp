package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"mserp/internal/groq"
	"mserp/internal/jobs"
	"mserp/internal/repository"
)

type Attachment struct {
	Filename string
	Data     []byte
	Caption  string
}
type ToolResult struct {
	Data       any
	Attachment *Attachment
	Pending    *repository.AssistantActionRequest
	Action     bool
	Before     any
}

type ToolExecutor struct {
	repo      *repository.AssistantRepository
	fleet     *repository.FleetRepository
	loads     *repository.LoadRepository
	fuel      *repository.FuelRepository
	tolls     *repository.TollRepository
	dashboard *repository.DashboardRepository
	loadJob   *jobs.SyncLoadsJob
	fuelJob   *jobs.SyncFuelJob
	tollJob   *jobs.SyncTollsJob
	now       func() time.Time
}

func NewToolExecutor(repo *repository.AssistantRepository, fleet *repository.FleetRepository,
	loads *repository.LoadRepository, fuel *repository.FuelRepository, tolls *repository.TollRepository,
	dashboard *repository.DashboardRepository, loadJob *jobs.SyncLoadsJob,
	fuelJob *jobs.SyncFuelJob, tollJob *jobs.SyncTollsJob) *ToolExecutor {
	return &ToolExecutor{repo: repo, fleet: fleet, loads: loads, fuel: fuel, tolls: tolls,
		dashboard: dashboard, loadJob: loadJob, fuelJob: fuelJob, tollJob: tollJob, now: time.Now}
}

func ToolDefinitions() []groq.AssistantTool {
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	stringField := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	return []groq.AssistantTool{
		tool("resolve_date_range", "Deterministically resolve a relative company reporting period before calling a report tool.", object(map[string]any{
			"period": map[string]any{"type": "string", "enum": []string{"today", "last_week", "this_week", "last_7_days"}},
		}, "period")),
		tool("list_fleet", "List or resolve drivers, trucks, or dispatchers. Use before actions and never guess IDs.", object(map[string]any{
			"entity":           map[string]any{"type": "string", "enum": []string{"driver", "truck", "dispatcher"}},
			"search":           stringField("Optional exact or partial name/unit search"),
			"include_inactive": map[string]any{"type": "boolean"},
		}, "entity")),
		tool("driver_fuel_report", "Detailed deterministic fuel, DEF, fee, gallon, savings, daily and transaction report for one driver.", object(map[string]any{
			"driver_id": stringField("Exact UUID returned by list_fleet"), "date_from": stringField("YYYY-MM-DD inclusive"), "date_to": stringField("YYYY-MM-DD inclusive"),
		}, "driver_id", "date_from", "date_to")),
		tool("fuel_report", "Company-wide or filtered fuel transaction report. Use driver_fuel_report when detailed category reconciliation for one driver is requested.", object(map[string]any{
			"driver": stringField("Optional canonical driver name returned by list_fleet"), "state": stringField("Optional two-letter state"), "category": map[string]any{"type": "string", "enum": []string{"", "fuel", "def", "other"}},
			"date_from": stringField("YYYY-MM-DD inclusive"), "date_to": stringField("YYYY-MM-DD inclusive"),
		}, "date_from", "date_to")),
		tool("financial_report", "Company, driver settlement, and dispatcher commission report for an exact date period.", object(map[string]any{
			"date_from": stringField("YYYY-MM-DD inclusive"), "date_to": stringField("YYYY-MM-DD inclusive"),
		}, "date_from", "date_to")),
		tool("load_report", "List and summarize loads with optional filters.", object(map[string]any{
			"search": stringField("Optional load/customer/name search"), "status": stringField("Optional status"), "driver": stringField("Optional canonical driver name"),
			"dispatcher": stringField("Optional canonical dispatcher name"), "date_from": stringField("Optional YYYY-MM-DD pickup date"), "date_to": stringField("Optional YYYY-MM-DD pickup date"),
		})),
		tool("toll_report", "List and summarize tolls with optional unit and date filters.", object(map[string]any{
			"search": stringField("Optional search"), "unit": stringField("Optional truck unit"), "date_from": stringField("Optional YYYY-MM-DD posting date"), "date_to": stringField("Optional YYYY-MM-DD posting date"),
		})),
		tool("sync_data", "Immediately synchronize one upstream source when the user explicitly commands it.", object(map[string]any{
			"source": map[string]any{"type": "string", "enum": []string{"loads", "fuel", "tolls"}},
		}, "source")),
		tool("create_fleet_record", "Create a driver, truck, or dispatcher from explicit supplied fields.", object(map[string]any{
			"entity": map[string]any{"type": "string", "enum": []string{"driver", "truck", "dispatcher"}}, "record": map[string]any{"type": "object"},
		}, "entity", "record")),
		tool("update_fleet_record", "Patch only explicitly requested fields on an exact driver, truck, or dispatcher ID.", object(map[string]any{
			"entity": map[string]any{"type": "string", "enum": []string{"driver", "truck", "dispatcher"}}, "id": stringField("Exact UUID returned by list_fleet"), "changes": map[string]any{"type": "object"},
		}, "entity", "id", "changes")),
		tool("delete_fleet_record", "Delete an exact driver, truck, or dispatcher. Always requires Telegram confirmation.", object(map[string]any{
			"entity": map[string]any{"type": "string", "enum": []string{"driver", "truck", "dispatcher"}}, "id": stringField("Exact UUID returned by list_fleet"),
		}, "entity", "id")),
	}
}

func tool(name, description string, parameters map[string]any) groq.AssistantTool {
	return groq.AssistantTool{Type: "function", Function: groq.AssistantToolDefinition{Name: name, Description: description, Parameters: parameters}}
}

func (e *ToolExecutor) Execute(ctx context.Context, identity repository.TelegramIdentity, name string, arguments json.RawMessage, confirmed bool) (ToolResult, error) {
	if len(arguments) == 0 || !json.Valid(arguments) {
		return ToolResult{}, errors.New("tool arguments must be valid JSON")
	}
	switch name {
	case "resolve_date_range":
		return e.resolveDateRange(arguments)
	case "list_fleet":
		return e.listFleet(ctx, arguments)
	case "driver_fuel_report":
		return e.driverFuelReport(ctx, arguments)
	case "fuel_report":
		return e.fuelReport(ctx, arguments)
	case "financial_report":
		return e.financialReport(ctx, arguments)
	case "load_report":
		return e.loadReport(ctx, arguments)
	case "toll_report":
		return e.tollReport(ctx, arguments)
	case "sync_data":
		return e.syncData(ctx, arguments)
	case "create_fleet_record":
		return e.createFleet(ctx, arguments)
	case "update_fleet_record":
		return e.updateFleet(ctx, identity, arguments, confirmed)
	case "delete_fleet_record":
		return e.deleteFleet(ctx, identity, arguments, confirmed)
	default:
		return ToolResult{}, fmt.Errorf("unknown assistant tool %q", name)
	}
}

func (e *ToolExecutor) fuelReport(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		Driver, State, Category string
		DateFrom                string `json:"date_from"`
		DateTo                  string `json:"date_to"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	from, to, err := parseDates(args.DateFrom, args.DateTo, false)
	if err != nil {
		return ToolResult{}, err
	}
	query := repository.FuelPageQuery{Pagination: repository.Pagination{Page: 1, PageSize: 100}, Driver: args.Driver, State: strings.ToUpper(args.State), Category: args.Category, DateFrom: from, DateTo: to}
	page, err := e.fuel.ListTransactionsPage(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	allRows := append([]repository.FuelTransaction(nil), page.Items...)
	for next := 2; next <= page.TotalPages; next++ {
		query.Pagination.Page = next
		nextPage, pageErr := e.fuel.ListTransactionsPage(ctx, query)
		if pageErr != nil {
			return ToolResult{}, pageErr
		}
		allRows = append(allRows, nextPage.Items...)
	}
	attachment, _ := csvAttachment(allRows, "fuel-"+args.DateFrom+"-"+args.DateTo+".csv", "Complete matching fuel report")
	fresh, _ := e.repo.DataFreshness(ctx, "fuel")
	return ToolResult{Data: map[string]any{"report": page, "dataFreshAsOf": fresh}, Attachment: attachment}, nil
}

func (e *ToolExecutor) resolveDateRange(raw json.RawMessage) (ToolResult, error) {
	var args struct {
		Period string `json:"period"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	today := companyToday(e.now())
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	daysSinceMonday := (int(today.Weekday()) + 6) % 7
	monday := today.AddDate(0, 0, -daysSinceMonday)
	from, to := today, today
	switch args.Period {
	case "today":
	case "this_week":
		from = monday
	case "last_week":
		from = monday.AddDate(0, 0, -7)
		to = monday.AddDate(0, 0, -1)
	case "last_7_days":
		from = today.AddDate(0, 0, -6)
	default:
		return ToolResult{}, errors.New("unsupported relative period")
	}
	return ToolResult{Data: map[string]string{"dateFrom": from.Format(time.DateOnly), "dateTo": to.Format(time.DateOnly), "timezone": "America/New_York"}}, nil
}

func (e *ToolExecutor) ExecuteConfirmed(ctx context.Context, identity repository.TelegramIdentity, request repository.AssistantActionRequest) (ToolResult, error) {
	current, err := e.currentState(ctx, request.ActionName, request.Arguments)
	if err != nil {
		return ToolResult{}, err
	}
	digest := sha256.Sum256(current)
	if request.BeforeHash == nil || *request.BeforeHash != hex.EncodeToString(digest[:]) {
		return ToolResult{}, errors.New("the record changed after the preview; request the action again")
	}
	return e.Execute(ctx, identity, request.ActionName, request.Arguments, true)
}

func (e *ToolExecutor) listFleet(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		Entity, Search  string
		IncludeInactive bool `json:"include_inactive"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	search := strings.ToLower(strings.TrimSpace(args.Search))
	match := func(values ...string) bool {
		if search == "" {
			return true
		}
		for _, v := range values {
			if strings.Contains(strings.ToLower(v), search) {
				return true
			}
		}
		return false
	}
	switch args.Entity {
	case "driver":
		values, err := e.fleet.ListDrivers(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		filtered := values[:0]
		for _, v := range values {
			if (args.IncludeInactive || v.Active) && match(v.FullName, v.ID) {
				filtered = append(filtered, v)
			}
		}
		return ToolResult{Data: filtered}, nil
	case "truck":
		values, err := e.fleet.ListTrucks(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		filtered := values[:0]
		for _, v := range values {
			if (args.IncludeInactive || v.Active) && match(v.UnitNumber, v.ID) {
				filtered = append(filtered, v)
			}
		}
		return ToolResult{Data: filtered}, nil
	case "dispatcher":
		values, err := e.fleet.ListDispatchers(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		filtered := values[:0]
		for _, v := range values {
			if (args.IncludeInactive || v.Active) && match(v.FullName, v.ID) {
				filtered = append(filtered, v)
			}
		}
		return ToolResult{Data: filtered}, nil
	default:
		return ToolResult{}, errors.New("entity must be driver, truck, or dispatcher")
	}
}

func parseDates(from, to string, optional bool) (*time.Time, *time.Time, error) {
	if optional && from == "" && to == "" {
		return nil, nil, nil
	}
	if from == "" || to == "" {
		return nil, nil, errors.New("both date_from and date_to are required")
	}
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return nil, nil, errors.New("date_from must use YYYY-MM-DD")
	}
	end, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return nil, nil, errors.New("date_to must use YYYY-MM-DD")
	}
	if start.After(end) {
		return nil, nil, errors.New("date_from cannot be after date_to")
	}
	if end.Sub(start) > 366*24*time.Hour {
		return nil, nil, errors.New("report ranges may not exceed 367 days")
	}
	return &start, &end, nil
}

func (e *ToolExecutor) driverFuelReport(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		DriverID string `json:"driver_id"`
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	from, to, err := parseDates(args.DateFrom, args.DateTo, false)
	if err != nil {
		return ToolResult{}, err
	}
	report, err := e.fuel.GetDriverFuelReport(ctx, args.DriverID, *from, *to)
	if err != nil {
		return ToolResult{}, err
	}
	attachment, err := csvAttachment(report.Transactions, "driver-fuel-"+args.DateFrom+"-"+args.DateTo+".csv", "Complete driver fuel transactions")
	if err != nil {
		return ToolResult{}, err
	}
	modelReport := report
	if len(modelReport.Transactions) > 100 {
		modelReport.Transactions = modelReport.Transactions[:100]
	}
	return ToolResult{Data: modelReport, Attachment: attachment}, nil
}

func (e *ToolExecutor) financialReport(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	from, to, err := parseDates(args.DateFrom, args.DateTo, false)
	if err != nil {
		return ToolResult{}, err
	}
	exclusive := to.AddDate(0, 0, 1)
	report, err := e.dashboard.GetFinancialDashboard(ctx, repository.FinancialDashboardQuery{DateFrom: from, DateTo: &exclusive})
	if err != nil {
		return ToolResult{}, err
	}
	rows := make([]any, 0, len(report.Drivers)+len(report.Dispatchers))
	for _, v := range report.Drivers {
		rows = append(rows, v)
	}
	for _, v := range report.Dispatchers {
		rows = append(rows, v)
	}
	attachment, _ := csvAttachment(rows, "financial-"+args.DateFrom+"-"+args.DateTo+".csv", "Financial breakdown")
	fresh, _ := e.repo.DataFreshness(ctx, "financial")
	return ToolResult{Data: map[string]any{"report": report, "dataFreshAsOf": fresh}, Attachment: attachment}, nil
}

func (e *ToolExecutor) loadReport(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		Search, Status, Driver, Dispatcher string
		DateFrom                           string `json:"date_from"`
		DateTo                             string `json:"date_to"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	from, to, err := parseDates(args.DateFrom, args.DateTo, true)
	if err != nil {
		return ToolResult{}, err
	}
	page, err := e.loads.GetLoadsPage(ctx, repository.LoadPageQuery{Pagination: repository.Pagination{Page: 1, PageSize: 100}, Search: args.Search, Status: args.Status, Driver: args.Driver, Dispatcher: args.Dispatcher, PickupFrom: from, PickupTo: to})
	if err != nil {
		return ToolResult{}, err
	}
	allRows := append([]repository.LoadRecord(nil), page.Items...)
	for next := 2; next <= page.TotalPages; next++ {
		nextPage, pageErr := e.loads.GetLoadsPage(ctx, repository.LoadPageQuery{Pagination: repository.Pagination{Page: next, PageSize: 100}, Search: args.Search, Status: args.Status, Driver: args.Driver, Dispatcher: args.Dispatcher, PickupFrom: from, PickupTo: to})
		if pageErr != nil {
			return ToolResult{}, pageErr
		}
		allRows = append(allRows, nextPage.Items...)
	}
	safeRows := sanitizeLoadRows(allRows)
	attachment, _ := csvAttachment(safeRows, "loads.csv", "Complete matching load report")
	fresh, _ := e.repo.DataFreshness(ctx, "loads")
	modelRows := safeRows
	if len(modelRows) > 100 {
		modelRows = modelRows[:100]
	}
	return ToolResult{Data: map[string]any{"items": modelRows, "total": page.Total, "summary": summarizeLoads(allRows), "dataFreshAsOf": fresh}, Attachment: attachment}, nil
}

func (e *ToolExecutor) tollReport(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		Search, Unit string
		DateFrom     string `json:"date_from"`
		DateTo       string `json:"date_to"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	from, to, err := parseDates(args.DateFrom, args.DateTo, true)
	if err != nil {
		return ToolResult{}, err
	}
	page, err := e.tolls.ListTollsPage(ctx, repository.TollPageQuery{Pagination: repository.Pagination{Page: 1, PageSize: 100}, Search: args.Search, Unit: args.Unit, PostFrom: from, PostTo: to})
	if err != nil {
		return ToolResult{}, err
	}
	allRows := append([]repository.Toll(nil), page.Items...)
	for next := 2; next <= page.TotalPages; next++ {
		nextPage, pageErr := e.tolls.ListTollsPage(ctx, repository.TollPageQuery{Pagination: repository.Pagination{Page: next, PageSize: 100}, Search: args.Search, Unit: args.Unit, PostFrom: from, PostTo: to})
		if pageErr != nil {
			return ToolResult{}, pageErr
		}
		allRows = append(allRows, nextPage.Items...)
	}
	attachment, _ := csvAttachment(allRows, "tolls.csv", "Complete matching toll report")
	fresh, _ := e.repo.DataFreshness(ctx, "tolls")
	return ToolResult{Data: map[string]any{"report": page, "dataFreshAsOf": fresh}, Attachment: attachment}, nil
}

func (e *ToolExecutor) syncData(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, err
	}
	switch args.Source {
	case "loads":
		v, err := e.loadJob.Run(ctx)
		return ToolResult{Data: v, Action: true}, err
	case "fuel":
		v, err := e.fuelJob.Run(ctx)
		return ToolResult{Data: v, Action: true}, err
	case "tolls":
		v, err := e.tollJob.Run(ctx)
		return ToolResult{Data: v, Action: true}, err
	default:
		return ToolResult{}, errors.New("source must be loads, fuel, or tolls")
	}
}

func csvAttachment(rows any, filename, caption string) (*Attachment, error) {
	data, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	var values []map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, nil
	}
	if len(values) == 0 {
		return nil, nil
	}
	keys := make([]string, 0)
	seen := map[string]bool{}
	for _, row := range values {
		for key := range row {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	_ = writer.Write(keys)
	for _, row := range values {
		record := make([]string, len(keys))
		for i, key := range keys {
			switch v := row[key].(type) {
			case nil:
			case string:
				record[i] = safeCSVCell(v)
			case float64:
				record[i] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				encoded, _ := json.Marshal(v)
				record[i] = string(encoded)
			}
		}
		_ = writer.Write(record)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return &Attachment{Filename: filename, Data: []byte(builder.String()), Caption: caption}, nil
}

func safeCSVCell(value string) string {
	if value == "" {
		return value
	}
	first := value[0]
	if first == '=' || first == '+' || first == '@' || first == '\t' || first == '\r' || (first == '-' && len(value) > 1 && (value[1] < '0' || value[1] > '9')) {
		return "'" + value
	}
	return value
}

func sanitizeLoadRows(rows []repository.LoadRecord) []map[string]any {
	values := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		encoded, _ := json.Marshal(row)
		var value map[string]any
		_ = json.Unmarshal(encoded, &value)
		delete(value, "RawPayload")
		values = append(values, value)
	}
	return values
}

func summarizeLoads(rows []repository.LoadRecord) map[string]any {
	gross := new(big.Rat)
	miles := new(big.Rat)
	for _, row := range rows {
		if value, ok := new(big.Rat).SetString(row.TotalPay); ok {
			gross.Add(gross, value)
		}
		if row.TotalMiles != nil {
			if value, ok := new(big.Rat).SetString(*row.TotalMiles); ok {
				miles.Add(miles, value)
			}
		}
	}
	return map[string]any{"loadCount": len(rows), "gross": gross.FloatString(2), "miles": miles.FloatString(2)}
}
