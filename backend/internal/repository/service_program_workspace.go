package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ServiceProgramWorkspace struct {
	Programs []ServiceProgram   `json:"programs"`
	Trucks   Page[ProgramTruck] `json:"trucks"`
}

// Summaries and visible rows share one snapshot and one due-state evaluation.
func (r *MaintenanceRepository) ProgramWorkspace(ctx context.Context, program, truck, search, status string, pagination Pagination) (ServiceProgramWorkspace, error) {
	out := ServiceProgramWorkspace{}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	out.Programs, err = servicePrograms(ctx, tx)
	if err != nil {
		return out, err
	}
	values, err := programTrucksWithPrograms(ctx, tx, "", "", maintenanceToday(), out.Programs)
	if err != nil {
		return out, err
	}
	indices := map[string]int{}
	for i, p := range out.Programs {
		indices[p.ID] = i
	}
	selected := []ProgramTruck{}
	search = strings.ToLower(search)
	for _, v := range values {
		if v.Enrolled && v.Active {
			i := indices[v.ProgramID]
			switch v.Status {
			case "upcoming":
				out.Programs[i].UpcomingCount++
			case "due", "overdue":
				out.Programs[i].OverdueCount++
			}
		}
		if program == "" && truck == "" {
			continue
		}
		if program != "" && v.ProgramID != program || truck != "" && v.TruckID != truck || !v.Enrolled || !v.Active && v.TruckOperatingStatus != "out_of_service" || status == "due" && v.Status != "due" && v.Status != "overdue" || !strings.Contains(strings.ToLower(v.UnitNumber+" "+v.ProgramName), search) {
			continue
		}
		selected = append(selected, v)
	}
	pagination = pagination.Normalize(len(selected))
	out.Trucks = NewPage(selected[pagination.Offset():min(pagination.Offset()+pagination.PageSize, len(selected))], len(selected), pagination)
	return out, tx.Commit(ctx)
}
