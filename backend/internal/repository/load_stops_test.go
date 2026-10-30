package repository

import (
	"reflect"
	"testing"
)

func TestLoadStops(t *testing.T) {
	raw := []byte(`[
	 {"stop_type":"delivery","ordering":4,"location":{"address1":" 900 Last St ","city":"Chicago","state":"IL","zip_code":"06001"}},
	 {"stop_type":"pickup","ordering":1,"location":{"address1":"10 First St","address2":" Dock 2 ","city":"Columbus","state":"OH","zip_code":"43215"}},
	 {"stop_type":"delivery","ordering":3,"location":null},
	 {"stop_type":"pickup","ordering":1,"location":{"address1":"20 Second St"}},
	 {"stop_type":"fuel","ordering":2,"location":{"address1":"Not a pickup or delivery"}}
	]`)
	want := []LoadStop{
		{Type: "pickup", Address: "10 First St, Dock 2", City: "Columbus", State: "OH", ZipCode: "43215"},
		{Type: "pickup", Address: "20 Second St"},
		{Type: "delivery"},
		{Type: "delivery", Address: "900 Last St", City: "Chicago", State: "IL", ZipCode: "06001"},
	}
	if got := loadStops(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("stops = %+v, want %+v", got, want)
	}
	for _, missing := range []string{"", "null", "[]", "{}", "invalid"} {
		if got := loadStops([]byte(missing)); got == nil || len(got) != 0 {
			t.Fatalf("missing %q = %+v, want empty array", missing, got)
		}
	}
}
