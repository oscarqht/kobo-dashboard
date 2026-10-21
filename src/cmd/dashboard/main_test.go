package main

import (
	"testing"
	"time"
)

func TestNextHourAtBoundaryAndDateRollover(t *testing.T) {
	for _, tc := range []struct{ name, now, want string }{
		{"subsecond", "2026-10-21T09:59:59.999999999+08:00", "2026-10-21T10:00:00+08:00"},
		{"exact boundary advances", "2026-10-21T10:00:00+08:00", "2026-10-21T11:00:00+08:00"},
		{"midnight", "2026-10-21T23:42:11+08:00", "2026-10-22T00:00:00+08:00"},
		{"year rollover", "2026-12-31T23:59:59Z", "2027-01-01T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339Nano, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			want, err := time.Parse(time.RFC3339Nano, tc.want)
			if err != nil {
				t.Fatal(err)
			}
			got := nextHourAt(now)
			if !got.Equal(want) || !got.After(now) || got.Sub(now) > time.Hour {
				t.Fatalf("nextHourAt(%v)=%v, want %v", now, got, want)
			}
		})
	}
}

func TestNextHourAtDSTDoesNotSkipOrRepeatScheduledInstant(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ now, want string }{
		{"2026-03-08T01:59:59-05:00", "2026-03-08T03:00:00-04:00"},
		{"2026-11-01T01:59:59-04:00", "2026-11-01T01:00:00-05:00"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		want, _ := time.Parse(time.RFC3339, tc.want)
		got := nextHourAt(now.In(loc))
		if !got.Equal(want) || got.Sub(now) != time.Second {
			t.Fatalf("DST schedule %v -> %v, want %v", now, got, want)
		}
	}
}
