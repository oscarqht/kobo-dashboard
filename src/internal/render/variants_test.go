package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"kobo-dashboard/src/internal/model"
)

func weatherFixture() model.Snapshot {
	s := model.Snapshot{Weather: map[string]model.Weather{}, Status: map[string]model.Status{}}
	d := model.Weather{Location: "Singapore", CurrentTemperature: 29, WeatherCode: 3}
	for i := 0; i < 4; i++ {
		d.Daily = append(d.Daily, model.WeatherDay{Date: fmt.Sprintf("2026-10-%02d", 21+i), Min: 24, Max: 31, RainProbability: 65})
	}
	s.Weather["w"] = d
	s.Status[model.WeatherStatusID("w")] = model.Status{UpdatedAt: time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)}
	return s
}
func assertBlankOutside(t *testing.T, c *canvas, r image.Rectangle) {
	t.Helper()
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			if r.Min.X <= x && x < r.Max.X && r.Min.Y <= y && y < r.Max.Y {
				continue
			}
			if c.img.RGBAAt(x, y) != (color.RGBA{255, 255, 255, 255}) {
				t.Fatalf("content outside widget at %d,%d (bounds %v)", x, y, r)
			}
		}
	}
}
func TestWeatherSizesRemainInsideTheirWidgets(t *testing.T) {
	for _, size := range []struct{ w, h int }{{12, 3}, {6, 3}, {4, 2}} {
		for _, units := range []string{"celsius", "fahrenheit"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.w, size.h, units), func(t *testing.T) {
				w := model.Widget{ID: "w", Type: "weather", X: 3, Y: 4, W: size.w, H: size.h, Units: units}
				if w.X+w.W > 12 {
					w.X = 0
				}
				r := widgetRect(w)
				c := newCanvas()
				defer c.close()
				c.loc = time.UTC
				s := weatherFixture()
				c.weather(w, s, r)
				assertBlankOutside(t, c, r.Inset(-1)) // Include its bottom one-pixel rule.
				if bytes.Equal(c.img.Pix, newCanvas().img.Pix) {
					t.Fatal("weather drew no data")
				}
			})
		}
	}
}
func TestWeatherMissingDailyAndStaleData(t *testing.T) {
	w := model.Widget{ID: "w", Type: "weather", W: 12, H: 3}
	r := widgetRect(w)
	for _, variant := range []string{"no-data", "no-daily", "invalid-date", "stale", "configured-name"} {
		t.Run(variant, func(t *testing.T) {
			s := weatherFixture()
			d := s.Weather["w"]
			switch variant {
			case "no-data":
				delete(s.Weather, "w")
			case "no-daily":
				d.Daily = nil
			case "invalid-date":
				d.Daily[0].Date = "unknown"
			case "stale":
				s.Status[model.WeatherStatusID("w")] = model.Status{UpdatedAt: time.Now(), Error: "offline"}
			case "configured-name":
				d.Location = ""
				w.Location = "A very long configured weather location that should be fitted inside its widget bounds"
			}
			if variant != "no-data" {
				s.Weather["w"] = d
			}
			c := newCanvas()
			defer c.close()
			c.loc = time.UTC
			c.weather(w, s, r)
			assertBlankOutside(t, c, r.Inset(-1))
		})
	}
}
func TestWeatherConditionCategories(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{{0, "Clear skies"}, {2, "Partly cloudy"}, {45, "Foggy"}, {63, "Rain"}, {73, "Snow"}, {81, "Rain showers"}, {85, "Snow showers"}, {95, "Thunderstorms"}} {
		if got := condition(tc.code); got != tc.want {
			t.Errorf("code %d: %q, want %q", tc.code, got, tc.want)
		}
	}
}
func crowdedSnapshot() model.Snapshot {
	s := model.Snapshot{Calendars: []model.Calendar{{ID: "selected", Summary: "Selected"}}, Status: map[string]model.Status{model.CalendarStatusID("selected"): {UpdatedAt: time.Now(), Error: "offline"}}}
	for i := 0; i < 14; i++ {
		s.Events = append(s.Events, model.Event{ID: fmt.Sprint(i), CalendarID: "selected", Title: fmt.Sprintf("Event %02d: a reasonably long title for the day detail view", i), Start: "2026-10-21", End: "2026-10-22", AllDay: true})
	}
	for i := 0; i < 20; i++ {
		s.Events = append(s.Events, model.Event{ID: fmt.Sprintf("hidden%d", i), CalendarID: "hidden", Title: "Hidden", Start: "2026-10-21", End: "2026-10-22", AllDay: true})
	}
	return s
}
func hasAction(hits []model.Hit, action string) bool {
	for _, h := range hits {
		if h.Action == action {
			return true
		}
	}
	return false
}
func TestCrowdedDayPaginationAndCalendarFilter(t *testing.T) {
	cfg := model.Config{Timezone: "Asia/Singapore", Widgets: []model.Widget{{ID: "c", Type: "calendar", Mode: "month", W: 12, H: 12, CalendarIDs: []string{"selected"}}}}
	s := crowdedSnapshot()
	v := model.View{Now: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), Day: "2026-10-21", WidgetID: "c"}
	for _, tc := range []struct {
		page           int
		previous, next bool
	}{{-1, false, true}, {0, false, true}, {1, true, true}, {2, true, false}, {100, true, false}} {
		v.DayPage = tc.page
		hits := Hits(cfg, s, v)
		if hasAction(hits, "day-previous") != tc.previous || hasAction(hits, "day-next") != tc.next {
			t.Fatalf("page %d targets: %#v", tc.page, hits)
		}
		if hasAction(hits, "reader") || hasAction(hits, "day") || !hasAction(hits, "close") {
			t.Fatal("day modal exposes background actions")
		}
	}
	// Twenty events on an unchecked calendar must not add pages.
	v.DayPage = 2
	filtered := s
	filtered.Events = append([]model.Event(nil), s.Events[:14]...)
	a, _ := Render(cfg, s, v)
	b, _ := Render(cfg, filtered, v)
	if !bytes.Equal(a, b) {
		t.Fatal("unselected calendar affected the visible snapshot")
	}
}
func TestDayModalEmptyInvalidAndLongTitles(t *testing.T) {
	cfg := model.DefaultConfig()
	v := model.View{Day: "2026-10-21", WidgetID: "calendar-1", Now: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
	for _, variant := range []string{"empty", "bad-widget", "bad-day", "bad-timezone", "long-title"} {
		t.Run(variant, func(t *testing.T) {
			config := cfg
			view := v
			s := model.Snapshot{}
			switch variant {
			case "bad-widget":
				view.WidgetID = "missing"
			case "bad-day":
				view.Day = "not-a-date"
			case "bad-timezone":
				config.Timezone = "Invalid/Timezone"
			case "long-title":
				for i := 0; i < 8; i++ {
					s.Events = append(s.Events, model.Event{CalendarID: "primary", Start: view.Day, End: "2026-10-22", AllDay: true, Title: strings.Repeat("A long event title that needs three lines to show completely ", 5)})
				}
			}
			c := build(config, s, view)
			defer c.close()
			if variant == "bad-widget" || variant == "bad-day" {
				if !hasAction(c.hits, "reader") {
					t.Fatal("invalid modal hid normal controls")
				}
			} else if !hasAction(c.hits, "close") {
				t.Fatal("modal has no close control")
			}
		})
	}
}
func TestMinimumMonthAndWeekWidgets(t *testing.T) {
	s := crowdedSnapshot()
	for _, mode := range []string{"week", "month"} {
		cfg := model.Config{Timezone: "Asia/Singapore", Widgets: []model.Widget{{ID: "c", Type: "calendar", W: 6, H: 4, Mode: mode, CalendarIDs: []string{"selected", "missing"}}}}
		v := model.View{Now: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
		c := build(cfg, s, v)
		defer c.close()
		if !hasAction(c.hits, "mode") || !hasAction(c.hits, "next") {
			t.Fatal("minimum widget lost navigation")
		}
		for _, h := range c.hits {
			if h.Action == "day" && !image.Rect(h.X, h.Y, h.X+h.W, h.Y+h.H).In(widgetRect(cfg.Widgets[0])) {
				t.Fatalf("day touch region outside minimum widget: %#v", h)
			}
		}
	}
}
func TestTimezoneDateChangesAndMissingNow(t *testing.T) {
	v := model.View{Now: time.Date(2026, 10, 20, 17, 0, 0, 0, time.UTC)}
	if got := nowFor(model.Config{Timezone: "Asia/Singapore"}, v).Format("2006-01-02"); got != "2026-10-21" {
		t.Fatal(got)
	}
	if got := nowFor(model.Config{Timezone: "bad-zone"}, v); !got.Equal(v.Now) {
		t.Fatal("invalid timezone should retain supplied instant")
	}
	if got := nowFor(model.Config{Timezone: "UTC"}, model.View{}); time.Since(got) > time.Minute {
		t.Fatal("missing view time did not use current time")
	}
}
func TestEventSortingAndMalformedProviderRecords(t *testing.T) {
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	w := model.Widget{CalendarIDs: []string{"c"}}
	events := []model.Event{{ID: "broken", CalendarID: "c", Start: "broken"}, {ID: "late", CalendarID: "c", Start: "2026-10-21T15:00:00Z", End: "2026-10-21T16:00:00Z"}, {ID: "all", CalendarID: "c", Start: "2026-10-21", End: "2026-10-22", AllDay: true}, {ID: "missing-end", CalendarID: "c", Start: "2026-10-21T09:00:00Z"}, {ID: "reversed", CalendarID: "c", Start: "2026-10-21T11:00:00Z", End: "2026-10-20T11:00:00Z"}}
	got := eventsForDay(events, w, day)
	want := []string{"all", "missing-end", "reversed", "late"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("event %d: %s, want %s", i, got[i].ID, id)
		}
	}
}
func TestBlankAndUnbrokenTitlesStayWithinCells(t *testing.T) {
	c := newCanvas()
	defer c.close()
	if got := c.wrap(" ", 150, 22, 2); len(got) != 1 || got[0] != "Untitled event" {
		t.Fatal(got)
	}
	if got := c.fit("too long", 1, 22, false); got != "" {
		t.Fatal(got)
	}
	for _, line := range c.wrap(strings.Repeat("A", 100)+" next word", 150, 22, 2) {
		if c.measure(line, 22, false) > 150 {
			t.Fatalf("unbroken word escaped its cell: %q", line)
		}
	}
}
