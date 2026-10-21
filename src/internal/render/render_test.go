package render

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"kobo-dashboard/src/internal/model"
)

func TestCalendarPeriodMondayAndLeapMonth(t *testing.T) {
	now := time.Date(2024, time.February, 29, 15, 0, 0, 0, time.UTC)
	w := model.Widget{ID: "c", Mode: "month"}
	v := model.View{Offsets: map[string]int{}}
	start, rows := period(w, v, now)
	if start.Format("2006-01-02") != "2024-01-29" || rows != 5 {
		t.Fatalf("leap month grid: %s, %d rows", start, rows)
	}
	w.Mode = "week"
	start, rows = period(w, v, now)
	if start.Weekday() != time.Monday || start.Format("2006-01-02") != "2024-02-26" || rows != 1 {
		t.Fatalf("week starts %v", start)
	}
	v.Offsets["c"] = 1
	start, _ = period(w, v, now)
	if start.Format("2006-01-02") != "2024-03-04" {
		t.Fatal(start)
	}
}
func TestEventsRespectLocalDatesAndExclusiveEnds(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Singapore")
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, loc)
	events := []model.Event{
		{ID: "all", CalendarID: "c", Start: "2026-10-20", End: "2026-10-21", AllDay: true},
		{ID: "utc", CalendarID: "c", Start: "2026-10-20T17:00:00Z", End: "2026-10-20T18:00:00Z"},
		{ID: "span", CalendarID: "c", Start: "2026-10-20T23:00:00+08:00", End: "2026-10-21T01:00:00+08:00"},
		{ID: "other", CalendarID: "private", Start: "2026-10-21", End: "2026-10-22", AllDay: true},
	}
	got := eventsForDay(events, model.Widget{CalendarIDs: []string{"c"}}, day)
	if len(got) != 2 {
		t.Fatalf("wanted two selected local events, got %#v", got)
	}
	for _, e := range got {
		if e.ID == "all" || e.ID == "other" {
			t.Fatalf("unexpected event %s", e.ID)
		}
	}
}
func TestRenderPNGAndTouchTargets(t *testing.T) {
	c := model.DefaultConfig()
	v := model.View{Now: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	s := model.Snapshot{}
	data, err := Render(c, s, v)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != Width || decoded.Bounds().Dy() != Height {
		t.Fatal(decoded.Bounds())
	}
	hits := Hits(c, s, v)
	dayCount := 0
	for _, h := range hits {
		if h.X < 0 || h.Y < 0 || h.X+h.W > Width || h.Y+h.H > Height {
			t.Fatalf("target outside screen: %#v", h)
		}
		if h.Action == "day" {
			dayCount++
			if _, err := time.Parse("2006-01-02", h.Value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if dayCount != 35 {
		t.Fatalf("expected 35 day targets for October 2026, got %d", dayCount)
	}
	v.PairingCode = "123456"
	v.EditorURL = "http://192.168.1.2:8080"
	hits = Hits(c, s, v)
	if len(hits) != 1 || hits[0].Action != "close" {
		t.Fatalf("pairing overlay must hide underlying controls: %#v", hits)
	}
}
func TestLongUnicodeTitlesRemainBounded(t *testing.T) {
	c := newCanvas()
	defer c.close()
	for _, title := range []string{"A very long event title that cannot fit into a single calendar cell", "家庭聚餐与朋友一起庆祝", "<script>alert('x')</script>"} {
		lines := c.wrap(title, 150, 22, 2)
		if len(lines) > 2 {
			t.Fatal(lines)
		}
		for _, line := range lines {
			if c.measure(line, 22, false) > 150 {
				t.Fatalf("overflow: %q", line)
			}
		}
	}
}
