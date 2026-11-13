package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/boardroutes"
)

type boardStopGeocode struct {
	state, postalCode string
}

func (s BoardStop) geocodeCoordinate() string {
	// Match the precision used in the TomTom request and share results across loads.
	return fmt.Sprintf("%.7f,%.7f", *s.Latitude, *s.Longitude)
}

func (g boardStopGeocode) label(address boardroutes.ReverseAddress) string {
	if g.state != "" && !strings.EqualFold(g.state, address.State) && !strings.EqualFold(g.state, address.StateName) {
		return ""
	}
	if g.postalCode != "" && boardPostalCode(g.postalCode) != boardPostalCode(address.PostalCode) {
		return ""
	}
	city := address.LocalName
	if city == "" {
		city = address.Municipality
	}
	if city == "" || address.State == "" {
		return ""
	}
	return city + ", " + address.State
}

func boardPostalCode(value string) string {
	base, _, _ := strings.Cut(value, "-")
	return strings.ToUpper(strings.Join(strings.Fields(base), ""))
}

// Only the live current and queued plans drive provider requests. Snapshot
// overlays can reuse those results wherever the same coordinates are visible.
func boardStopGeocodeTargets(board DriverBoard) map[string]BoardStop {
	targets := map[string]BoardStop{}
	for _, driver := range board.Drivers {
		if driver.Departed {
			continue
		}
		view := board.Loads[driver.ID]
		add := func(load BoardLoad) {
			if load.LoadID == nil || load.SourceMissing {
				return
			}
			for _, stop := range load.Stops {
				if stop.geocode != nil {
					targets[stop.geocodeCoordinate()] = stop
				}
			}
		}
		if view.Current != nil {
			add(*view.Current)
		}
		for _, load := range view.Next {
			add(load)
		}
	}
	return targets
}

func (r *DriverBoardRepository) ReconcileStopCities(ctx context.Context, routes *boardroutes.Client) error {
	if routes == nil || !routes.Options.Enabled || routes.Options.TomTomAPIKey == "" || routes.Options.ReverseGeocodeMonthlyLimit <= 0 {
		return nil
	}
	week, _ := time.Parse(time.DateOnly, boardLoadToday(r.now()))
	board, err := r.get(ctx, week, false)
	if err != nil {
		return err
	}
	targets := boardStopGeocodeTargets(board)
	if len(targets) == 0 {
		return nil
	}
	keys := make([]string, 0, len(targets))
	for key := range targets {
		keys = append(keys, key)
	}
	rows, err := r.pool.Query(ctx, `SELECT k.coordinate FROM unnest($1::text[]) k(coordinate)
 LEFT JOIN board_stop_geocodes c USING(coordinate)
 WHERE c.next_attempt_at IS NULL OR c.next_attempt_at <= now()
 ORDER BY c.next_attempt_at NULLS FIRST,k.coordinate LIMIT 4`, keys)
	if err != nil {
		return err
	}
	due, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, key := range due {
		// Claim before HTTP to prevent duplicate billed requests and retry storms.
		var retryAt time.Time
		err = r.pool.QueryRow(ctx, `INSERT INTO board_stop_geocodes(coordinate,next_attempt_at)
 VALUES($1,now()+interval '1 hour') ON CONFLICT(coordinate) DO UPDATE
 SET next_attempt_at=EXCLUDED.next_attempt_at WHERE board_stop_geocodes.next_attempt_at<=now()
 RETURNING next_attempt_at`, key).Scan(&retryAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		stop := targets[key]
		address, lookupErr := routes.ReverseGeocodeAddress(ctx, *stop.Latitude, *stop.Longitude)
		if lookupErr != nil {
			// Keep the fallback and let the existing provider throttle settle.
			return nil
		}
		if _, err = r.pool.Exec(ctx, `UPDATE board_stop_geocodes SET address=$2,next_attempt_at=now()+interval '30 days'
 WHERE coordinate=$1 AND next_attempt_at=$3`, key, address, retryAt); err != nil {
			return err
		}
	}
	return nil
}

func applyBoardStopCities(ctx context.Context, tx pgx.Tx, views map[string]BoardLoads) error {
	keys := map[string]bool{}
	for _, view := range views {
		visitBoardStopCityLoads(view, func(load BoardLoad) {
			for _, stop := range load.Stops {
				if stop.geocode != nil {
					keys[stop.geocodeCoordinate()] = true
				}
			}
		})
	}
	if len(keys) == 0 {
		return nil
	}
	coordinates := make([]string, 0, len(keys))
	for key := range keys {
		coordinates = append(coordinates, key)
	}
	rows, err := tx.Query(ctx, `SELECT coordinate,address FROM board_stop_geocodes WHERE coordinate=ANY($1::text[]) AND address IS NOT NULL`, coordinates)
	if err != nil {
		return err
	}
	defer rows.Close()
	addresses := map[string]boardroutes.ReverseAddress{}
	for rows.Next() {
		var key string
		var address boardroutes.ReverseAddress
		if err := rows.Scan(&key, &address); err != nil {
			rows.Close()
			return err
		}
		addresses[key] = address
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for id, view := range views {
		if view.Current != nil {
			current := *view.Current
			current.Stops = boardStopsWithCities(current.Stops, addresses)
			view.Current = &current
			for _, stop := range current.Stops {
				if stop.Key == view.StopKey && view.DestinationSource {
					view.SourceDestination = stop.Location
					if view.Automation != nil && view.Automation.MapRoute != nil {
						view.Automation.MapRoute.Destination = stop.Location
					}
					break
				}
			}
		}
		for _, loads := range [][]BoardLoad{view.Week, view.Next, view.Earlier, view.Hidden, view.Unavailable} {
			for i := range loads {
				loads[i].Stops = boardStopsWithCities(loads[i].Stops, addresses)
			}
		}
		views[id] = view
	}
	return nil
}

func visitBoardStopCityLoads(view BoardLoads, visit func(BoardLoad)) {
	if view.Current != nil {
		visit(*view.Current)
	}
	for _, loads := range [][]BoardLoad{view.Week, view.Next, view.Earlier, view.Hidden, view.Unavailable} {
		for _, load := range loads {
			visit(load)
		}
	}
}

func boardStopsWithCities(stops []BoardStop, addresses map[string]boardroutes.ReverseAddress) []BoardStop {
	var updated []BoardStop
	for i, stop := range stops {
		if stop.geocode == nil {
			continue
		}
		if label := stop.geocode.label(addresses[stop.geocodeCoordinate()]); label != "" {
			if updated == nil {
				updated = slices.Clone(stops)
			}
			updated[i].Location = label
		}
	}
	if updated != nil {
		return updated
	}
	return stops
}
