package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kobo-dashboard/src/internal/model"
)

func TestCalendarPaginationExpansionAndTokenReuse(t *testing.T) {
	tokens, events := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokens++
			r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" {
				t.Error("wrong grant")
			}
			w.Write([]byte(`{"access_token":"private","expires_in":3600}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer private" {
			t.Error("missing bearer")
		}
		if r.URL.Path == "/users/me/calendarList" {
			if r.URL.Query().Get("pageToken") == "" {
				w.Write([]byte(`{"items":[{"id":"primary","summary":"Personal"}],"nextPageToken":"second"}`))
			} else {
				w.Write([]byte(`{"items":[{"id":"shared","summary":"Family"}]}`))
			}
			return
		}
		events++
		q := r.URL.Query()
		if q.Get("singleEvents") != "true" || q.Get("orderBy") != "startTime" || q.Get("timeZone") != "Asia/Singapore" {
			t.Error("recurrence or timezone missing")
		}
		if q.Get("pageToken") == "" {
			w.Write([]byte(`{"items":[{"id":"a","summary":"Trip","start":{"date":"2026-10-21"},"end":{"date":"2026-10-23"}}],"nextPageToken":"next"}`))
		} else {
			w.Write([]byte(`{"items":[{"id":"b","summary":"Meeting","start":{"dateTime":"2026-10-23T09:00:00+08:00"},"end":{"dateTime":"2026-10-23T10:00:00+08:00"}},{"id":"c","status":"cancelled"}]}`))
		}
	}))
	defer srv.Close()
	g := NewGoogle(srv.Client(), Credentials{ClientID: "client", RefreshToken: "refresh"})
	g.BaseURL = srv.URL
	g.TokenEndpoint = srv.URL + "/token"
	calendars, err := g.Calendars(context.Background())
	if err != nil || len(calendars) != 2 {
		t.Fatalf("calendars %v %v", calendars, err)
	}
	now := time.Now()
	es, err := g.Events(context.Background(), []string{"primary", "primary"}, now, now.AddDate(0, 1, 0), "Asia/Singapore")
	if err != nil || len(es) != 2 || !es[0].AllDay || es[1].Title != "Meeting" {
		t.Fatalf("events %v %v", es, err)
	}
	if tokens != 1 || events != 2 {
		t.Fatalf("token reuse or pagination failed: %d %d", tokens, events)
	}
}
func TestTokenErrorsDoNotLeakResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"secret-token"}`))
	}))
	defer srv.Close()
	_, err := Exchange(context.Background(), srv.Client(), srv.URL, url.Values{})
	if !errors.Is(err, ErrAuthorization) || strings.Contains(err.Error(), "secret-token") {
		t.Fatal(err)
	}
}
func TestCredentialsExclusiveAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	c := Credentials{ClientID: "id", RefreshToken: "secret"}
	if err := WriteCredentials(path, c); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredentials(path, Credentials{}); err == nil {
		t.Fatal("overwrote existing credentials")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	got, err := ReadCredentials(path)
	if err != nil || got != c {
		t.Fatal(got, err)
	}
}
func TestWeatherForecastAndMissingData(t *testing.T) {
	incomplete := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("temperature_unit") != "celsius" || r.URL.Query().Get("forecast_days") != "3" {
			t.Error("bad query")
		}
		if incomplete {
			w.Write([]byte(`{"current":{"temperature_2m":30,"weather_code":0},"daily":{"time":[]}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"current": map[string]any{"temperature_2m": 30, "weather_code": 2}, "daily": map[string]any{"time": []string{"2026-10-21", "2026-10-22", "2026-10-23"}, "temperature_2m_min": []float64{25, 24, 25}, "temperature_2m_max": []float64{31, 32, 30}, "precipitation_probability_max": []float64{80, 50, 30}, "weather_code": []int{2, 3, 61}}})
	}))
	defer srv.Close()
	provider := NewWeather(srv.Client())
	provider.ForecastURL = srv.URL
	widget := model.Widget{Location: "Singapore", Latitude: 1.3, Longitude: 103.8}
	got, err := provider.Forecast(context.Background(), widget, "Asia/Singapore")
	if err != nil || len(got.Daily) != 3 || got.Daily[0].RainProbability != 80 {
		t.Fatal(got, err)
	}
	incomplete = true
	if _, err := provider.Forecast(context.Background(), widget, "Asia/Singapore"); err == nil {
		t.Fatal("accepted missing forecast")
	}
}
func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	_, err := Exchange(context.Background(), source.Client(), source.URL, url.Values{"refresh_token": {"secret"}})
	if err == nil || reached {
		t.Fatal("followed credential redirect")
	}
}
func TestAuthorizationURLUsesPKCE(t *testing.T) {
	u, _ := url.Parse(AuthorizationURL("client", "http://127.0.0.1:1234/callback", "state", "verifier"))
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") != "state" || q.Get("access_type") != "offline" || q.Get("scope") != CalendarScope {
		t.Fatal(q)
	}
}
