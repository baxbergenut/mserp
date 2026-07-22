package jobs

import (
	"testing"
	"time"
)

func TestNextDailyRun(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "later today",
			now:  time.Date(2026, time.July, 22, 1, 15, 0, 0, location),
			want: time.Date(2026, time.July, 22, 3, 0, 0, 0, location),
		},
		{
			name: "tomorrow after scheduled time",
			now:  time.Date(2026, time.July, 22, 3, 1, 0, 0, location),
			want: time.Date(2026, time.July, 23, 3, 0, 0, 0, location),
		},
		{
			name: "tomorrow at scheduled time",
			now:  time.Date(2026, time.July, 22, 3, 0, 0, 0, location),
			want: time.Date(2026, time.July, 23, 3, 0, 0, 0, location),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nextDailyRun(test.now, location, 3, 0)
			if !got.Equal(test.want) {
				t.Fatalf("nextDailyRun() = %s, want %s", got, test.want)
			}
		})
	}
}
