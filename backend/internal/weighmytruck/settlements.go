package weighmytruck

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Ticket retains the vendor's calendar date, not the server's timezone.
type Ticket struct {
	Number, OriginalNumber, Date, Time, Fee, Tractor, Trailer, DriverCode, Site, Location, City, State string
	Raw                                                                                                map[string]string
}

func (c *Client) FetchSettlements(ctx context.Context, date string) ([]Ticket, error) {
	if !c.Configured() {
		return nil, errors.New("WeighMyTruck is not configured on the server")
	}
	token, err := c.bearer(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"types": []int{3}, "date": date, "includeDriverCode": true, "startTime": "00:00", "endTime": "23:59"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.options.APIURL, "/")+"/Settlement/GetSettlement", strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.New("Could not prepare settlement request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("WeighMyTruck settlement service unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("WeighMyTruck settlement request returned HTTP %d", res.StatusCode)
	}
	var files struct {
		CSV []byte `json:"csv"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&files); err != nil {
		return nil, errors.New("Invalid WeighMyTruck settlement response")
	}
	if len(files.CSV) == 0 {
		return []Ticket{}, nil
	}
	return ParseTickets(string(files.CSV))
}

var feePattern = regexp.MustCompile(`^-?\d{1,9}(\.\d{1,2})?$`)

func ParseTickets(data string) ([]Ticket, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(data, "\ufeff")))
	headers, err := reader.Read()
	if err != nil {
		return nil, errors.New("Invalid WeighMyTruck CSV header")
	}
	columns := map[string]int{}
	for i, h := range headers {
		columns[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, h := range []string{"date", "ticket number", "fee", "tractor number", "site number"} {
		if _, ok := columns[h]; !ok {
			return nil, fmt.Errorf("WeighMyTruck CSV is missing %s", h)
		}
	}
	result := []Ticket{}
	seen := map[string]bool{}
	for rowNo := 2; ; rowNo++ {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("Invalid WeighMyTruck CSV row %d", rowNo)
		}
		get := func(names ...string) string {
			for _, name := range names {
				if i, ok := columns[name]; ok {
					return strings.TrimSpace(row[i])
				}
			}
			return ""
		}
		t := Ticket{Number: get("ticket number"), OriginalNumber: get("original ticket number"), Time: get("approx time", "approximate time"), Fee: get("fee"), Tractor: get("tractor number"), Trailer: get("trailer number"), DriverCode: get("driver code"), Site: get("site number"), Location: get("t/s name", "truck stop name"), City: get("city"), State: get("st", "state"), Raw: map[string]string{}}
		for i, h := range headers {
			if h != "" {
				t.Raw[h] = row[i]
			}
		}
		var date time.Time
		for _, layout := range []string{"01-02-06", "01/02/2006", "2006-01-02", "01-02-2006"} {
			date, e = time.Parse(layout, get("date"))
			if e == nil {
				break
			}
		}
		if e != nil || t.Number == "" || t.Site == "" || !feePattern.MatchString(t.Fee) {
			return nil, fmt.Errorf("Invalid WeighMyTruck ticket at CSV row %d", rowNo)
		}
		t.Date = date.Format(time.DateOnly)
		key := t.Site + ":" + t.Number
		if seen[key] {
			return nil, errors.New("Duplicate ticket in WeighMyTruck response")
		}
		seen[key] = true
		result = append(result, t)
	}
	return result, nil
}
