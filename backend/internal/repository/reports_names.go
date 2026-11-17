package repository

import (
	"context"
	"strings"
)

// Keep profile IDs intact while displaying the recorded first name in Reports.
func (s *reportRead) reportFirstNames(ctx context.Context, result []ReportRow) error {
	ids := map[string]bool{}
	for id := range s.drivers {
		ids[id] = true
	}
	for _, row := range result {
		for _, driver := range row.AssignedDrivers {
			ids[driver.ID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	rows, err := s.tx.Query(ctx, `SELECT id::text,full_name,coalesce(nullif(trim(first_name),''),split_part(trim(full_name),' ',1)) FROM drivers WHERE id=ANY($1::uuid[])`, keys)
	if err != nil {
		return err
	}
	defer rows.Close()
	names, full := map[string]string{}, map[string]string{}
	for rows.Next() {
		var id, name, first string
		if err = rows.Scan(&id, &name, &first); err != nil {
			return err
		}
		names[id] = first
		full[name] = first
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for id := range s.drivers {
		if name, ok := names[id]; ok {
			s.drivers[id] = name
		}
	}
	for n := range result {
		row := &result[n]
		if s.query.View == ReportDriver {
			if name, ok := names[row.ID]; ok {
				row.Name = name
			}
		}
		for j := range row.AssignedDrivers {
			if name, ok := names[row.AssignedDrivers[j].ID]; ok {
				row.AssignedDrivers[j].Name = name
			}
		}
		for j := range row.Sources {
			source := &row.Sources[j]
			original := source.Driver
			if name, ok := full[source.Driver]; ok {
				source.Driver = name
			}
			if source.Kind == "Driver Pay" {
				driver, _, _ := strings.Cut(source.ID, "/")
				if name, ok := names[driver]; ok {
					source.Driver = name
					if source.Label == original {
						source.Label = name
					}
				}
			}
		}
	}
	return nil
}
