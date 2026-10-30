package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

type StatusColors struct {
	// Legacy keys are background hues. Shared .fg/.bg keys preserve explicit
	// foreground/background pairs in the existing versioned JSON and audit.
	Colors  map[string]string `json:"colors"`
	Version int               `json:"version"`
}

var statusColorHex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var ErrStatusColorsConflict = errors.New("status colors changed; reload saved colors before editing again")
var statusColorKeys = strings.Fields(`truck_ready driver_ready active in_use shop out_of_service terminated home vacation rest family_issue health_issue new_driver covered dispatched enroute reserved deadhead stop yard hold_for_load pickup_delivery_issue accident rejected canceled no_load stuck late_delivery truck_issue open in_progress completed paid pending needs_review discarded disabled partial scheduled unpaid failed`)

func validStatusColors(s StatusColors) bool {
	if s.Version < 1 || s.Version > 2147483646 || s.Colors == nil || len(s.Colors) > 3*len(statusColorKeys) {
		return false
	}
	allowed := map[string]bool{}
	for _, key := range statusColorKeys {
		allowed[key] = true
	}
	for key, value := range s.Colors {
		base, part, hasPart := strings.Cut(key, ".")
		if !allowed[base] || (hasPart && part != "fg" && part != "bg") || !statusColorHex.MatchString(value) {
			return false
		}
	}
	return true
}

func (r *FleetRepository) StatusColors(ctx context.Context) (StatusColors, error) {
	var s StatusColors
	err := r.pool.QueryRow(ctx, `SELECT colors,version FROM status_color_settings WHERE singleton`).Scan(&s.Colors, &s.Version)
	return s, err
}
func (r *FleetRepository) SaveStatusColors(ctx context.Context, s StatusColors, actor string) (StatusColors, error) {
	if !validStatusColors(s) {
		return s, chargeInvalid("Choose a valid status and a six-digit hex color")
	}
	for key, value := range s.Colors {
		s.Colors[key] = strings.ToLower(value)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return s, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE status_color_settings SET colors=$1,version=version+1,updated_at=now(),updated_by=nullif($3,'')::uuid WHERE singleton AND version=$2 RETURNING colors,version`, s.Colors, s.Version, actor).Scan(&s.Colors, &s.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrStatusColorsConflict
	}
	if err != nil {
		return s, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO status_color_history(version,colors,actor_id) VALUES($1,$2,nullif($3,'')::uuid)`, s.Version, s.Colors, actor)
	if err != nil {
		return s, err
	}
	return s, tx.Commit(ctx)
}
