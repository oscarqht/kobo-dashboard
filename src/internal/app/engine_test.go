package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"kobo-dashboard/src/internal/model"
	"kobo-dashboard/src/internal/providers"
)

type engineTransport func(*http.Request) (*http.Response, error)

func (f engineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func engineResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const forecastJSON = `{"current":{"temperature_2m":30,"weather_code":2},"daily":{"time":["2026-10-21","2026-10-22","2026-10-23"],"temperature_2m_min":[24,25,26],"temperature_2m_max":[30,31,32],"precipitation_probability_max":[80,50,30],"weather_code":[2,3,61]}}`

func TestRefreshRetainsFailedSourcesAndUpdatesIndependentSources(t *testing.T) {
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	e.Config.Widgets[0].Location = "Singapore"
	e.Config.Widgets[1].CalendarIDs = []string{"primary", "family"}
	old := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	e.Snapshot.Events = []model.Event{{ID: "old-p", CalendarID: "primary", Title: "Keep me", Start: "2026-10-21", End: "2026-10-22", AllDay: true}, {ID: "old-f", CalendarID: "family", Title: "Replace me"}}
	e.Snapshot.Weather["weather-1"] = model.Weather{Location: "Singapore", CurrentTemperature: 22}
	for _, key := range []string{"primary", "family", "weather-1"} {
		statusKey := model.CalendarStatusID(key)
		if key == "weather-1" {
			statusKey = model.WeatherStatusID(key)
		}
		e.Snapshot.Status[statusKey] = model.Status{UpdatedAt: old}
	}
	e.SetCredentials(providers.Credentials{ClientID: "client-private", ClientSecret: "secret-private", RefreshToken: "refresh-private"})
	weatherFails := false
	e.client = &http.Client{Transport: engineTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "oauth2.googleapis.com":
			return engineResponse(200, `{"access_token":"access-private","expires_in":3600}`), nil
		case strings.HasSuffix(r.URL.Path, "calendarList"):
			return engineResponse(200, `{"items":[{"id":"primary","summary":"Personal"},{"id":"family","summary":"Family"}]}`), nil
		case strings.Contains(r.URL.Path, "/calendars/primary/"):
			return engineResponse(403, `{"error":"secret-private"}`), nil
		case strings.Contains(r.URL.Path, "/calendars/family/"):
			if r.URL.Query().Get("singleEvents") != "true" || r.URL.Query().Get("timeZone") != "Asia/Singapore" {
				t.Error("missing expansion or timezone")
			}
			from, err := time.Parse(time.RFC3339, r.URL.Query().Get("timeMin"))
			if err != nil {
				t.Error(err)
			}
			_, offset := from.Zone()
			if offset != 8*3600 || from.Hour() != 0 {
				t.Error("query must use local midnight", from)
			}
			return engineResponse(200, `{"items":[{"id":"new-f","summary":"Updated title","start":{"date":"2026-10-21"},"end":{"date":"2026-10-23"}}]}`), nil
		case r.URL.Host == "api.open-meteo.com":
			if weatherFails {
				return engineResponse(503, "private-server-error"), nil
			}
			return engineResponse(200, forecastJSON), nil
		default:
			t.Errorf("unexpected request %s", r.URL)
			return engineResponse(500, ""), nil
		}
	})}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, s, _ := e.State()
	if len(s.Events) != 2 {
		t.Fatalf("wrong event count %v", s.Events)
	}
	foundOld, foundNew := false, false
	for _, ev := range s.Events {
		if ev.ID == "old-p" && ev.Title == "Keep me" {
			foundOld = true
		}
		if ev.ID == "new-f" && ev.AllDay && ev.End == "2026-10-23" {
			foundNew = true
		}
		if ev.ID == "old-f" {
			t.Fatal("successful calendar retained obsolete events")
		}
	}
	if !foundOld || !foundNew {
		t.Fatal("independent calendar refresh failed", s.Events)
	}
	if !s.Status[model.CalendarStatusID("primary")].UpdatedAt.Equal(old) || s.Status[model.CalendarStatusID("primary")].Error == "" {
		t.Fatal("failed calendar lost last success", s.Status)
	}
	if !s.Status[model.CalendarStatusID("family")].UpdatedAt.After(old) || s.Status[model.CalendarStatusID("family")].Error != "" {
		t.Fatal("successful calendar not fresh", s.Status)
	}
	if s.Weather["weather-1"].CurrentTemperature != 30 || s.Status[model.WeatherStatusID("weather-1")].Error != "" {
		t.Fatal("calendar error blocked weather", s)
	}
	lastWeather := s.Status[model.WeatherStatusID("weather-1")].UpdatedAt
	weatherFails = true
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, s, _ = e.State()
	if s.Weather["weather-1"].CurrentTemperature != 30 || !s.Status[model.WeatherStatusID("weather-1")].UpdatedAt.Equal(lastWeather) || s.Status[model.WeatherStatusID("weather-1")].Error == "" {
		t.Fatal("failed weather discarded cache", s)
	}
	raw, err := os.ReadFile(filepath.Join(e.Dir, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"client-private", "secret-private", "refresh-private", "access-private", "private-server-error"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private provider data persisted", secret)
		}
	}
}

func TestSaveRejectsInvalidConfigWithoutReplacingCurrentState(t *testing.T) {
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	before, _, _ := e.State()
	bad := before
	bad.Timezone = "not/a/timezone"
	if err := e.Save(bad); err == nil {
		t.Fatal("accepted invalid timezone")
	}
	after, _, _ := e.State()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid save mutated config")
	}
	if _, err := os.Stat(filepath.Join(e.Dir, "config.json")); !os.IsNotExist(err) {
		t.Fatal("invalid config reached disk", err)
	}
	bad = before
	bad.Widgets = append([]model.Widget(nil), before.Widgets...)
	bad.Widgets[1].Y = 0
	if err := e.Save(bad); err == nil {
		t.Fatal("accepted overlapping widgets")
	}
}

func TestStateAPIHasNoCredentialsOrProviderSecrets(t *testing.T) {
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	e.SetCredentials(providers.Credentials{ClientID: "client-private", ClientSecret: "secret-private", RefreshToken: "refresh-private"})
	server, err := NewServer(e, false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://localhost/api/state", nil)
	req.AddCookie(&http.Cookie{Name: "dashboard_session", Value: server.token})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"client-private", "secret-private", "refresh-private", "client_secret", "refresh_token"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatal("private credentials exposed", secret)
		}
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["config"] == nil || data["snapshot"] == nil {
		t.Fatal("state response missing public configuration")
	}
}

func TestMissingCredentialsAndNetworkErrorKeepSnapshot(t *testing.T) {
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	e.Snapshot.Events = []model.Event{{ID: "saved", CalendarID: "primary", Title: "Offline event"}}
	e.Snapshot.Status[model.CalendarStatusID("primary")] = model.Status{UpdatedAt: old}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.MarkNetworkError()
	_, s, _ := e.State()
	if len(s.Events) != 1 || s.Events[0].ID != "saved" || !s.Status[model.CalendarStatusID("primary")].UpdatedAt.Equal(old) || s.Status[model.CalendarStatusID("primary")].Error == "" {
		t.Fatal("offline state lost cache", s)
	}
}

func TestStateAndSaveDoNotAliasCallerSlices(t *testing.T) {
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	e.Snapshot.Weather["weather-1"] = model.Weather{Daily: []model.WeatherDay{{Date: "2026-10-21", Max: 31}}}
	c, s, _ := e.State()
	c.Widgets[1].CalendarIDs[0] = "changed"
	weather := s.Weather["weather-1"]
	weather.Daily[0].Max = 999
	fresh, snapshot, _ := e.State()
	if fresh.Widgets[1].CalendarIDs[0] != "primary" || snapshot.Weather["weather-1"].Daily[0].Max != 31 {
		t.Fatal("State leaked mutable slices")
	}
	if err := e.Save(fresh); err != nil {
		t.Fatal(err)
	}
	fresh.Widgets[1].CalendarIDs[0] = "changed after save"
	after, _, _ := e.State()
	if after.Widgets[1].CalendarIDs[0] != "primary" {
		t.Fatal("Save retained caller-owned slice")
	}
}

func TestModeActionPersistsAndReturnsToCurrentPeriod(t *testing.T) {
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct{ action, value string }{{"next", ""}, {"day", "2026-10-21"}, {"day-next", ""}} {
		if err := e.Action(action.action, "calendar-1", action.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Action("mode", "calendar-1", "week"); err != nil {
		t.Fatal(err)
	}
	c, _, v := e.State()
	if c.Widgets[1].Mode != "week" || len(v.Offsets) != 0 || v.Day != "" || v.DayPage != 0 {
		t.Fatal("mode change did not reset browsing", c, v)
	}
	restarted, err := New(e.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _, _ := restarted.State()
	if persisted.Widgets[1].Mode != "week" {
		t.Fatal("mode not persisted")
	}
	if err := e.Action("mode", "weather-1", "week"); err == nil {
		t.Fatal("weather accepted calendar mode")
	}
	if err := e.Action("mode", "calendar-1", "agenda"); err == nil {
		t.Fatal("accepted unsupported mode")
	}
	if err := e.Action("next", "calendar-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Action("today", "", ""); err != nil {
		t.Fatal(err)
	}
	_, _, v = e.State()
	if len(v.Offsets) != 0 {
		t.Fatal("global Today did not reset browsing")
	}
}

func TestWeatherSettingsInvalidateOldLocationOrUnitCache(t *testing.T) {
	for _, change := range []string{"location", "coordinates", "units"} {
		t.Run(change, func(t *testing.T) {
			e, err := New(t.TempDir(), false)
			if err != nil {
				t.Fatal(err)
			}
			e.Config.Widgets[0].Location = "Singapore"
			e.Snapshot.Weather["weather-1"] = model.Weather{Location: "Singapore", CurrentTemperature: 30}
			e.Snapshot.Status[model.WeatherStatusID("weather-1")] = model.Status{UpdatedAt: time.Now()}
			c, _, _ := e.State()
			switch change {
			case "location":
				c.Widgets[0].Location = "London"
			case "coordinates":
				c.Widgets[0].Latitude = 51.5
			case "units":
				c.Widgets[0].Units = "fahrenheit"
			}
			if err := e.Save(c); err != nil {
				t.Fatal(err)
			}
			_, s, _ := e.State()
			if _, ok := s.Weather["weather-1"]; ok {
				t.Fatal("old forecast shown under new settings")
			}
			if _, ok := s.Status[model.WeatherStatusID("weather-1")]; ok {
				t.Fatal("new settings retain old success timestamp")
			}
		})
	}
}
