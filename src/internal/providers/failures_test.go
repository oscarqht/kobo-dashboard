package providers

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
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

type failedTransport struct{}

func (failedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport-secret-refresh-token")
}

func TestLocationSearchQueryResultsAndFailures(t *testing.T) {
	mode := "success"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "Singapore" || r.URL.Query().Get("count") != "10" || r.URL.Query().Get("language") != "en" {
			t.Error("incorrect geocoding request", r.URL.Query())
		}
		switch mode {
		case "success":
			w.Write([]byte(`{"results":[{"name":"Singapore","latitude":1.2897,"longitude":103.8501,"timezone":"Asia/Singapore","country":"Singapore"}]}`))
		case "empty":
			w.Write([]byte(`{}`))
		case "http":
			w.WriteHeader(503)
			w.Write([]byte("provider-secret"))
		case "json":
			w.Write([]byte(`not-json-provider-secret`))
		}
	}))
	defer srv.Close()
	provider := NewWeather(srv.Client())
	provider.GeocodeURL = srv.URL
	results, err := provider.Search(context.Background(), " Singapore ")
	if err != nil || len(results) != 1 || results[0].Timezone != "Asia/Singapore" || results[0].Latitude != 1.2897 {
		t.Fatal(results, err)
	}
	mode = "empty"
	results, err = provider.Search(context.Background(), "Singapore")
	if err != nil || results == nil || len(results) != 0 {
		t.Fatal("empty results must be []", results, err)
	}
	for _, name := range []string{"", "a", strings.Repeat("b", 201)} {
		if _, err := provider.Search(context.Background(), name); err == nil {
			t.Fatal("accepted invalid search length")
		}
	}
	for _, state := range []string{"http", "json"} {
		mode = state
		_, err = provider.Search(context.Background(), "Singapore")
		if err == nil || strings.Contains(err.Error(), "provider-secret") {
			t.Fatal("geocoding error leaked body", err)
		}
	}
	provider.Client = &http.Client{Transport: failedTransport{}}
	if _, err := provider.Search(context.Background(), "Singapore"); err == nil || strings.Contains(err.Error(), "transport-secret") {
		t.Fatal("transport detail leaked", err)
	}
}

func TestWeatherValidationMissingValuesAndFahrenheit(t *testing.T) {
	body := `{"current":{"temperature_2m":86,"weather_code":0},"daily":{"time":["2026-10-21","2026-10-22","2026-10-23"],"temperature_2m_min":[75,76,77],"temperature_2m_max":[86,87,88],"precipitation_probability_max":[0,20,30],"weather_code":[0,1,2]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("temperature_unit") != "fahrenheit" {
			t.Error("wrong unit")
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()
	provider := NewWeather(srv.Client())
	provider.ForecastURL = srv.URL
	widget := model.Widget{Location: "Singapore", Units: "fahrenheit", Latitude: 1.3, Longitude: 103.8}
	result, err := provider.Forecast(context.Background(), widget, "Asia/Singapore")
	if err != nil || result.CurrentTemperature != 86 {
		t.Fatal(result, err)
	}
	for _, bad := range []struct {
		name     string
		widget   model.Widget
		timezone string
	}{
		{"empty location", model.Widget{}, "UTC"},
		{"NaN latitude", model.Widget{Location: "City", Latitude: math.NaN()}, "UTC"},
		{"latitude range", model.Widget{Location: "City", Latitude: 91}, "UTC"},
		{"longitude range", model.Widget{Location: "City", Longitude: -181}, "UTC"},
		{"bad timezone", widget, "unknown/zone"},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := provider.Forecast(context.Background(), bad.widget, bad.timezone); err == nil {
				t.Fatal("accepted invalid forecast input")
			}
		})
	}
	for _, badBody := range []string{
		strings.Replace(body, `"temperature_2m":86`, `"temperature_2m":null`, 1),
		strings.Replace(body, `"weather_code":0`, `"weather_code":null`, 1),
		strings.Replace(body, `[75,76,77]`, `[75,null,77]`, 1),
		strings.Replace(body, `2026-10-21`, `invalid-date`, 1),
		strings.Replace(body, `[0,20,30]`, `[0,20]`, 1),
	} {
		body = badBody
		if _, err := provider.Forecast(context.Background(), widget, "UTC"); err == nil {
			t.Fatal("accepted malformed/missing forecast values")
		}
	}
}

func TestTokenAndCredentialInputFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"malformed", "private-not-json", 200}, {"empty", `{}`, 200}, {"missing expiry", `{"access_token":"private"}`, 200}, {"other OAuth error", `{"error":"invalid_client","error_description":"private"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer srv.Close()
			_, err := Exchange(context.Background(), srv.Client(), srv.URL, url.Values{})
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid token error or leaked body", err)
			}
		})
	}
	_, err := Exchange(context.Background(), &http.Client{Transport: failedTransport{}}, "https://oauth2.googleapis.com/token", url.Values{})
	if err == nil || strings.Contains(err.Error(), "transport-secret") {
		t.Fatal(err)
	}
	if _, err := Exchange(context.Background(), nil, "://invalid", url.Values{}); err == nil {
		t.Fatal("accepted invalid endpoint")
	}
	dir := t.TempDir()
	for _, tc := range []struct{ name, body string }{{"json", "{"}, {"missing client", `{"refresh_token":"private"}`}, {"missing refresh", `{"client_id":"id"}`}} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCredentials(path); err == nil {
			t.Fatal("accepted invalid credentials", tc.name)
		}
	}
	if _, err := ReadCredentials(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing credentials accepted")
	}
	if err := WriteCredentials(filepath.Join(dir, "missing", "credentials.json"), Credentials{}); err == nil {
		t.Fatal("unavailable destination accepted")
	}
}

func TestRandomOAuthSecretsAreURLSafeAndIndependent(t *testing.T) {
	a, err := RandomSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomSecret()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil || len(decoded) != 32 || a == b || len(a) < 43 {
		t.Fatal("OAuth state/verifier lacks random URL-safe bytes")
	}
}

func TestGoogleRejectsInvalidRangesAndMissingAuthorization(t *testing.T) {
	g := NewGoogle(&http.Client{Transport: failedTransport{}}, Credentials{})
	now := time.Now()
	if _, err := g.Events(context.Background(), []string{"primary"}, now, now, "UTC"); err == nil {
		t.Fatal("accepted empty range")
	}
	if _, err := g.Events(context.Background(), []string{"primary"}, now, now.Add(time.Hour), "unknown/zone"); err == nil {
		t.Fatal("accepted invalid timezone")
	}
	if _, err := g.Calendars(context.Background()); !errors.Is(err, ErrAuthorization) {
		t.Fatal("missing credentials did not request reauthorization", err)
	}
	empty, err := g.Events(context.Background(), []string{""}, now, now.Add(time.Hour), "UTC")
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}

func TestGooglePaginationLoopsAreRejected(t *testing.T) {
	for _, events := range []bool{false, true} {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				w.Write([]byte(`{"access_token":"private","expires_in":3600}`))
				return
			}
			requests++
			w.Write([]byte(`{"items":[],"nextPageToken":"loop"}`))
		}))
		g := NewGoogle(srv.Client(), Credentials{ClientID: "id", RefreshToken: "refresh"})
		g.BaseURL = srv.URL
		g.TokenEndpoint = srv.URL + "/token"
		var err error
		if events {
			_, err = g.Events(context.Background(), []string{"primary"}, time.Now(), time.Now().Add(time.Hour), "UTC")
		} else {
			_, err = g.Calendars(context.Background())
		}
		srv.Close()
		if err == nil || requests != 2 {
			t.Fatal("pagination loop not rejected", events, requests, err)
		}
	}
}

func TestGoogleHTTPAndTransportErrorsAreSanitized(t *testing.T) {
	status := 401
	body := "private-details"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"private","expires_in":3600}`))
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	g := NewGoogle(srv.Client(), Credentials{ClientID: "id", RefreshToken: "refresh"})
	g.BaseURL = srv.URL
	g.TokenEndpoint = srv.URL + "/token"
	if _, err := g.Calendars(context.Background()); !errors.Is(err, ErrAuthorization) {
		t.Fatal(err)
	}
	status = 503
	if _, err := g.Calendars(context.Background()); err == nil || strings.Contains(err.Error(), "private-details") {
		t.Fatal(err)
	}
	status = 200
	body = "malformed-private-details"
	if _, err := g.Calendars(context.Background()); err == nil || strings.Contains(err.Error(), "private-details") {
		t.Fatal(err)
	}
	g.Client = &http.Client{Transport: failedTransport{}}
	if _, err := g.Calendars(context.Background()); err == nil || strings.Contains(err.Error(), "transport-secret") {
		t.Fatal(err)
	}
}
