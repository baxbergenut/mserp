package datatruck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ModifiedDatetime is transport metadata, not financial source evidence. Keep it
// out of the canonical payload so unrelated provider edits do not invalidate pay.
func (l *Load) UnmarshalJSON(data []byte) error {
	type wire Load
	var value struct {
		wire
		ModifiedDatetime *time.Time `json:"modified_datetime"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*l = Load(value.wire)
	l.ModifiedDatetime = value.ModifiedDatetime
	return nil
}

func (c *Client) LoadSyncScope() string { return c.baseURL }

// FetchModifiedLoadsPage always fetches page one with an increasing ID bound.
// Offset pagination can skip records while orders change or disappear. A fixed
// lower timestamp and ID keyset avoid that; edits to earlier IDs are picked up
// by the next sweep, whose checkpoint is this sweep's START (never its end).
func (c *Client) FetchModifiedLoadsPage(ctx context.Context, since time.Time, afterID int) ([]Load, error) {
	if since.IsZero() || afterID < 0 {
		return nil, fmt.Errorf("invalid modified-load cursor")
	}
	filters, err := json.Marshal([]map[string]string{
		{"column": "modified_datetime", "contains": "after", "value": since.UTC().Format(time.RFC3339Nano)},
		{"column": "id", "contains": "greater_than", "value": strconv.Itoa(afterID + 1)},
	})
	if err != nil {
		return nil, err
	}
	query := url.Values{"page_size": {"25"}, "ordering": {"id"}, "filter": {string(filters)}}
	page, err := c.doRequest(ctx, c.baseURL+"/orders/?"+query.Encode())
	if err != nil {
		return nil, err
	}
	if len(page.Results) > 25 || (len(page.Results) < 25 && (page.Count > len(page.Results) || (page.Next != nil && *page.Next != ""))) {
		return nil, fmt.Errorf("unexpected modified-load page size")
	}
	previousID := afterID
	for _, load := range page.Results {
		if load.ID <= previousID || load.ModifiedDatetime == nil || load.ModifiedDatetime.Before(since) {
			return nil, fmt.Errorf("datatruck modified filter/order returned invalid record %d", load.ID)
		}
		previousID = load.ID
	}
	return page.Results, nil
}
