package app

import (
	"context"
	"fmt"
	"kobo-dashboard/src/internal/model"
	"kobo-dashboard/src/internal/providers"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

type Engine struct {
	mu          sync.Mutex
	refreshMu   sync.Mutex
	fetchCancel context.CancelFunc
	Config      model.Config
	Snapshot    model.Snapshot
	View        model.View
	Dir         string
	Demo        bool
	Changed     chan struct{}
	Exit        chan struct{}
	credentials *providers.Credentials
	client      *http.Client
}

func New(dir string, demo bool) (*Engine, error) {
	e := &Engine{Dir: dir, Demo: demo, Changed: make(chan struct{}, 1), Exit: make(chan struct{}, 1), client: &http.Client{Timeout: 25 * time.Second}}
	e.Config = model.DefaultConfig()
	if err := readJSON(filepath.Join(dir, "config.json"), &e.Config); err != nil && !missing(err) {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := e.Config.Validate(); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "snapshot.json"), &e.Snapshot); err != nil && !missing(err) {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	if demo || e.Snapshot.Demo {
		e.Snapshot = model.Snapshot{}
	}
	if e.Snapshot.Weather == nil {
		e.Snapshot.Weather = map[string]model.Weather{}
	}
	if e.Snapshot.Status == nil {
		e.Snapshot.Status = map[string]model.Status{}
	}
	e.View = model.View{Offsets: map[string]int{}, Now: time.Now()}
	if demo {
		e.makeDemo()
	}
	return e, nil
}
func (e *Engine) Notify() {
	select {
	case e.Changed <- struct{}{}:
	default:
	}
}
func (e *Engine) State() (model.Config, model.Snapshot, model.View) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Deep copy: render and HTTP serialization must never race refresh/config changes.
	c := e.Config
	s := e.Snapshot
	v := e.View
	c.Widgets = cloneWidgets(c.Widgets)
	s.Events = append([]model.Event(nil), s.Events...)
	s.Calendars = append([]model.Calendar(nil), s.Calendars...)
	s.Weather = map[string]model.Weather{}
	for k, x := range e.Snapshot.Weather {
		x.Daily = append([]model.WeatherDay(nil), x.Daily...)
		s.Weather[k] = x
	}
	s.Status = map[string]model.Status{}
	for k, x := range e.Snapshot.Status {
		s.Status[k] = x
	}
	v.Offsets = map[string]int{}
	for k, x := range e.View.Offsets {
		v.Offsets[k] = x
	}
	v.Now = time.Now()
	return c, s, v
}
func (e *Engine) Save(c model.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	e.mu.Lock()
	if e.fetchCancel != nil {
		e.fetchCancel()
	}
	e.mu.Unlock()
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	if err := writeJSON(filepath.Join(e.Dir, "config.json"), c); err != nil {
		return err
	}
	e.mu.Lock()
	for _, w := range c.Widgets {
		for _, old := range e.Config.Widgets {
			if old.ID == w.ID && w.Type == "weather" && (old.Location != w.Location || old.Latitude != w.Latitude || old.Longitude != w.Longitude || old.Units != w.Units || e.Config.Timezone != c.Timezone) {
				delete(e.Snapshot.Weather, w.ID)
				delete(e.Snapshot.Status, model.WeatherStatusID(w.ID))
			}
		}
	}
	c.Widgets = cloneWidgets(c.Widgets)
	e.Config = c
	err := writeJSON(filepath.Join(e.Dir, "snapshot.json"), e.Snapshot)
	e.mu.Unlock()
	e.Notify()
	return err
}
func (e *Engine) ResetView() {
	e.mu.Lock()
	e.View.Offsets = map[string]int{}
	e.View.Day = ""
	e.View.WidgetID = ""
	e.View.DayPage = 0
	e.mu.Unlock()
	e.Notify()
}
func (e *Engine) Action(action, id, value string) error {
	if action == "today" && id == "" {
		e.ResetView()
		return nil
	}
	if action == "mode" {
		c, _, _ := e.State()
		found := false
		for i := range c.Widgets {
			if c.Widgets[i].ID == id && c.Widgets[i].Type == "calendar" {
				c.Widgets[i].Mode = value
				found = true
			}
		}
		if !found {
			return fmt.Errorf("choose a calendar")
		}
		if err := e.Save(c); err != nil {
			return err
		}
		e.ResetView()
		return nil
	}
	if action == "reader" {
		select {
		case e.Exit <- struct{}{}:
		default:
		}
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var widget *model.Widget
	for i := range e.Config.Widgets {
		if e.Config.Widgets[i].ID == id {
			widget = &e.Config.Widgets[i]
			break
		}
	}
	switch action {
	case "previous", "next", "today":
		if widget == nil || widget.Type != "calendar" {
			return fmt.Errorf("choose a calendar")
		}
		n := e.View.Offsets[id]
		if action == "previous" {
			n--
		}
		if action == "next" {
			n++
		}
		if action == "today" {
			n = 0
		}
		if n < -12 || n > 12 {
			return fmt.Errorf("navigation is limited to twelve periods")
		}
		e.View.Offsets[id] = n
	case "day":
		if widget == nil || widget.Type != "calendar" {
			return fmt.Errorf("choose a calendar")
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return fmt.Errorf("invalid day")
		}
		e.View.Day = value
		e.View.WidgetID = id
		e.View.DayPage = 0
	case "day-next", "day-previous":
		if e.View.Day == "" {
			return fmt.Errorf("open a day first")
		}
		if action == "day-next" && e.View.DayPage < 10000 {
			e.View.DayPage++
		}
		if action == "day-previous" && e.View.DayPage > 0 {
			e.View.DayPage--
		}
	case "close":
		e.View.Day = ""
		e.View.WidgetID = ""
	default:
		return fmt.Errorf("unknown action")
	}
	e.Notify()
	return nil
}
func (e *Engine) makeDemo() {
	now := time.Now()
	e.Snapshot.Demo = true
	e.Snapshot.Calendars = []model.Calendar{{ID: "primary", Summary: "Personal"}, {ID: "family", Summary: "Family"}}
	for i := 0; i < 24; i++ {
		d := now.AddDate(0, 0, i-5)
		titles := []string{"Design review", "Dinner with friends", "Morning walk", "Plan the week", "Family lunch", "Book club"}
		e.Snapshot.Events = append(e.Snapshot.Events, model.Event{ID: fmt.Sprint(i), CalendarID: "primary", Title: titles[i%len(titles)], Start: d.Format("2006-01-02"), End: d.AddDate(0, 0, 1).Format("2006-01-02"), AllDay: true})
	}
	e.Snapshot.Status[model.CalendarStatusID("primary")] = model.Status{UpdatedAt: now}
	e.Snapshot.Status[model.CalendarStatusID("family")] = model.Status{UpdatedAt: now}
	for i := range e.Config.Widgets {
		w := &e.Config.Widgets[i]
		if w.Type != "weather" {
			continue
		}
		if w.Location == "" {
			w.Location = "Singapore"
			w.Latitude = 1.29
			w.Longitude = 103.85
		}
		weather := model.Weather{Location: w.Location, CurrentTemperature: 29, WeatherCode: 3}
		for j := 0; j < 3; j++ {
			weather.Daily = append(weather.Daily, model.WeatherDay{Date: now.AddDate(0, 0, j).Format("2006-01-02"), Min: 25, Max: 31, RainProbability: 65, WeatherCode: 3})
		}
		e.Snapshot.Weather[w.ID] = weather
		e.Snapshot.Status[model.WeatherStatusID(w.ID)] = model.Status{UpdatedAt: now}
	}
}
func (e *Engine) SetCredentials(c providers.Credentials) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.credentials = &c
}
func (e *Engine) Refresh(ctx context.Context) error {
	if !e.refreshMu.TryLock() {
		return nil
	}
	defer e.refreshMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.fetchCancel = cancel
	e.mu.Unlock()
	defer func() { cancel(); e.mu.Lock(); e.fetchCancel = nil; e.mu.Unlock() }()
	if e.Demo {
		e.mu.Lock()
		e.Snapshot = model.Snapshot{Weather: map[string]model.Weather{}, Status: map[string]model.Status{}}
		e.makeDemo()
		e.mu.Unlock()
		e.Notify()
		return nil
	}
	c, _, v := e.State()
	loc, _ := time.LoadLocation(c.Timezone)
	now := time.Now().In(loc)
	e.mu.Lock()
	var creds *providers.Credentials
	if e.credentials != nil {
		copy := *e.credentials
		creds = &copy
	}
	e.mu.Unlock()
	// Fetch each calendar independently so a failed source retains its last successful data.
	ids := map[string]bool{}
	for _, w := range c.Widgets {
		if w.Type == "calendar" {
			for _, id := range w.CalendarIDs {
				ids[id] = true
			}
		}
	}
	if creds != nil {
		g := providers.NewGoogle(e.client, *creds)
		calendars, err := g.Calendars(ctx)
		if err == nil {
			e.mu.Lock()
			e.Snapshot.Calendars = calendars
			e.mu.Unlock()
		}
		for id := range ids {
			start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -1, 0)
			end := start.AddDate(0, 3, 0)
			for _, w := range c.Widgets {
				if w.Type != "calendar" {
					continue
				}
				offset := v.Offsets[w.ID]
				anchor := now
				if w.Mode == "week" {
					anchor = now.AddDate(0, 0, offset*7)
				} else {
					anchor = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, offset, 0)
				}
				if anchor.Before(start) {
					start = anchor.AddDate(0, 0, -7)
				}
				if anchor.After(end.AddDate(0, 0, -35)) {
					end = anchor.AddDate(0, 2, 0)
				}
			}
			events, err := g.Events(ctx, []string{id}, start, end, c.Timezone)
			e.mu.Lock()
			status := e.Snapshot.Status[model.CalendarStatusID(id)]
			if err != nil {
				status.Error = "Calendar refresh failed; reconnect Google if authorization expired"
			} else {
				remaining := e.Snapshot.Events[:0]
				for _, ev := range e.Snapshot.Events {
					if ev.CalendarID != id {
						remaining = append(remaining, ev)
					}
				}
				e.Snapshot.Events = append(remaining, events...)
				status = model.Status{UpdatedAt: time.Now()}
			}
			e.Snapshot.Status[model.CalendarStatusID(id)] = status
			e.mu.Unlock()
		}
	} else {
		e.mu.Lock()
		for id := range ids {
			s := e.Snapshot.Status[model.CalendarStatusID(id)]
			s.Error = "Connect Google using the USB setup helper"
			e.Snapshot.Status[model.CalendarStatusID(id)] = s
		}
		e.mu.Unlock()
	}
	weather := providers.NewWeather(e.client)
	for _, w := range c.Widgets {
		if w.Type != "weather" {
			continue
		}
		var data model.Weather
		var err error
		if w.Location == "" {
			err = fmt.Errorf("choose a weather location in the editor")
		} else {
			data, err = weather.Forecast(ctx, w, c.Timezone)
		}
		e.mu.Lock()
		s := e.Snapshot.Status[model.WeatherStatusID(w.ID)]
		if err != nil {
			s.Error = "Weather unavailable; check location and Wi-Fi"
			if w.Location == "" {
				s.Error = err.Error()
			}
		} else {
			e.Snapshot.Weather[w.ID] = data
			s = model.Status{UpdatedAt: time.Now()}
		}
		e.Snapshot.Status[model.WeatherStatusID(w.ID)] = s
		e.mu.Unlock()
	}
	e.mu.Lock()
	err := writeJSON(filepath.Join(e.Dir, "snapshot.json"), e.Snapshot)
	e.mu.Unlock()
	e.Notify()
	return err
}

func (e *Engine) SetEditor(code, url string) {
	e.mu.Lock()
	e.View.PairingCode = code
	e.View.EditorURL = url
	e.mu.Unlock()
	e.Notify()
}
func (e *Engine) MarkNetworkError() {
	e.mu.Lock()
	for _, w := range e.Config.Widgets {
		if w.Type == "weather" {
			s := e.Snapshot.Status[model.WeatherStatusID(w.ID)]
			s.Error = "Wi-Fi unavailable; showing last successful data"
			e.Snapshot.Status[model.WeatherStatusID(w.ID)] = s
		} else {
			for _, id := range w.CalendarIDs {
				s := e.Snapshot.Status[model.CalendarStatusID(id)]
				s.Error = "Wi-Fi unavailable; showing last successful data"
				e.Snapshot.Status[model.CalendarStatusID(id)] = s
			}
		}
	}
	e.mu.Unlock()
	e.Notify()
}

func cloneWidgets(in []model.Widget) []model.Widget {
	out := append([]model.Widget(nil), in...)
	for i := range out {
		out[i].CalendarIDs = append([]string(nil), out[i].CalendarIDs...)
	}
	return out
}
