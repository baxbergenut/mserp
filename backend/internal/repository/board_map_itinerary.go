package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"

	"mserp/internal/boardroutes"
)

type BoardMapLeg struct {
	Kind       string                    `json:"kind"`
	LoadNumber string                    `json:"loadNumber"`
	Miles      float64                   `json:"miles"`
	Geometry   boardroutes.RouteGeometry `json:"geometry"`
}

type boardMapWaypoint struct {
	boardroutes.Waypoint
	Kind, LoadNumber, LoadKey, StopKey string
}

// The live ETA ends at the selected stop. The rest of the itinerary is for
// display, and follows the board's actual stop and queue order.
func boardMapItinerary(view BoardLoads, active BoardStop) ([]boardMapWaypoint, string) {
	if view.Current == nil || view.Current.SourceMissing || active.Latitude == nil || active.Longitude == nil {
		return nil, ""
	}
	var waypoints []boardMapWaypoint
	previous := boardroutes.Waypoint{Latitude: *active.Latitude, Longitude: *active.Longitude}
	appendStop := func(load BoardLoad, stop BoardStop, kind string) bool {
		if stop.Latitude == nil || stop.Longitude == nil {
			return false
		}
		point := boardroutes.Waypoint{Latitude: *stop.Latitude, Longitude: *stop.Longitude}
		if !(math.Abs(point.Latitude) <= 90 && math.Abs(point.Longitude) <= 180) || point == (boardroutes.Waypoint{}) {
			return false
		}
		if point != previous {
			waypoints = append(waypoints, boardMapWaypoint{point, kind, load.Number, boardCompletionKey(load), stop.Key})
			previous = point
		}
		return true
	}
	seenActive, currentComplete := false, true
	lastType := ""
	for _, stop := range view.Current.Stops {
		if !seenActive {
			seenActive = stop.Key == active.Key
		} else if !appendStop(*view.Current, stop, "current") {
			currentComplete = false
			break
		}
		lastType = stop.Type
	}
	if seenActive && currentComplete && lastType == "delivery" && len(view.Next) > 0 && !view.Next[0].SourceMissing {
		next := view.Next[0]
		for i, stop := range next.Stops {
			kind := "next-load"
			if i == 0 {
				if stop.Type != "pickup" {
					break
				}
				kind = "deadhead"
			}
			if !appendStop(next, stop, kind) {
				break
			}
		}
	}
	if len(waypoints) == 0 || len(waypoints) > 150 {
		return nil, ""
	}
	encoded, _ := json.Marshal(waypoints)
	digest := sha256.Sum256(encoded)
	return waypoints, hex.EncodeToString(digest[:])
}
