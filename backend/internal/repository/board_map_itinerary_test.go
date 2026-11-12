package repository

import (
	"reflect"
	"testing"
)

func TestBoardMapItineraryUsesCurrentStopAndFirstQueuedLoad(t *testing.T) {
	stop := func(key, kind string, lat, lng float64) BoardStop {
		return BoardStop{Key: key, Type: kind, Latitude: &lat, Longitude: &lng}
	}
	active := stop("a-pu", "pickup", 40, -74)
	view := BoardLoads{
		Current: &BoardLoad{PlanID: "a", Number: "A", Stops: []BoardStop{active, stop("a-del", "delivery", 41, -73)}},
		Next: []BoardLoad{
			{PlanID: "b", Number: "B", Stops: []BoardStop{stop("b-pu", "pickup", 42, -72), stop("b-del", "delivery", 43, -71)}},
			{PlanID: "c", Number: "C", Stops: []BoardStop{stop("c-pu", "pickup", 44, -70), stop("c-del", "delivery", 45, -69)}},
		},
	}
	points, key := boardMapItinerary(view, active)
	var order []string
	for _, point := range points {
		order = append(order, point.Kind+"/"+point.StopKey)
	}
	if key == "" || !reflect.DeepEqual(order, []string{"current/a-del", "deadhead/b-pu", "next-load/b-del"}) {
		t.Fatalf("unexpected itinerary: %v", order)
	}
	view.Next[0], view.Next[1] = view.Next[1], view.Next[0]
	if _, reordered := boardMapItinerary(view, active); reordered == key {
		t.Fatal("queue reorder must invalidate cached following legs")
	}
	view.Current.Stops[1].Latitude = nil
	if points, key = boardMapItinerary(view, active); len(points) != 0 || key != "" {
		t.Fatalf("itinerary skipped a current stop with unknown coordinates: %v", points)
	}
}
