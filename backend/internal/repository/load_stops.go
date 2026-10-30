package repository

import (
	"encoding/json"
	"sort"
	"strings"
)

// LoadStop exposes only the address fields needed by the Loads directory.
type LoadStop struct {
	Type    string `json:"type"`
	Address string `json:"address"`
	City    string `json:"city"`
	State   string `json:"state"`
	ZipCode string `json:"zipCode"`
}

func loadStops(raw []byte) []LoadStop {
	result := []LoadStop{}
	var source []struct {
		Type     string `json:"stop_type"`
		Ordering int    `json:"ordering"`
		Location struct {
			Address1 string `json:"address1"`
			Address2 string `json:"address2"`
			City     string `json:"city"`
			State    string `json:"state"`
			ZipCode  string `json:"zip_code"`
		} `json:"location"`
	}
	if json.Unmarshal(raw, &source) != nil {
		return result
	}
	sort.SliceStable(source, func(i, j int) bool { return source[i].Ordering < source[j].Ordering })
	for _, stop := range source {
		kind := strings.ToLower(strings.TrimSpace(stop.Type))
		if kind != "pickup" && kind != "delivery" {
			continue
		}
		address := []string{}
		for _, line := range []string{stop.Location.Address1, stop.Location.Address2} {
			if line = strings.TrimSpace(line); line != "" {
				address = append(address, line)
			}
		}
		result = append(result, LoadStop{
			Type: kind, Address: strings.Join(address, ", "),
			City: strings.TrimSpace(stop.Location.City), State: strings.TrimSpace(stop.Location.State),
			ZipCode: strings.TrimSpace(stop.Location.ZipCode),
		})
	}
	return result
}
