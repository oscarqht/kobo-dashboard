package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kobo-dashboard/src/internal/model"
)

type Location struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Country   string  `json:"country"`
}
type Weather struct {
	Client                  *http.Client
	ForecastURL, GeocodeURL string
}

func NewWeather(client *http.Client) *Weather {
	return &Weather{Client: safeClient(client), ForecastURL: "https://api.open-meteo.com/v1/forecast", GeocodeURL: "https://geocoding-api.open-meteo.com/v1/search"}
}
func (w *Weather) get(ctx context.Context, endpoint string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return errors.New("invalid weather request")
	}
	resp, err := safeClient(w.Client).Do(req)
	if err != nil {
		return errors.New("weather connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("weather provider returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out); err != nil {
		return errors.New("invalid weather response")
	}
	return nil
}
func (w *Weather) Search(ctx context.Context, name string) ([]Location, error) {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 200 {
		return nil, errors.New("location search needs 2 to 200 characters")
	}
	var response struct {
		Results []Location `json:"results"`
	}
	if err := w.get(ctx, w.GeocodeURL, url.Values{"name": {name}, "count": {"10"}, "language": {"en"}, "format": {"json"}}, &response); err != nil {
		return nil, err
	}
	if response.Results == nil {
		response.Results = []Location{}
	}
	return response.Results, nil
}
func (w *Weather) Forecast(ctx context.Context, widget model.Widget, timezone string) (model.Weather, error) {
	var result model.Weather
	if widget.Location == "" {
		return result, errors.New("choose a weather location in the editor")
	}
	if math.IsNaN(widget.Latitude) || math.IsNaN(widget.Longitude) || widget.Latitude < -90 || widget.Latitude > 90 || widget.Longitude < -180 || widget.Longitude > 180 {
		return result, errors.New("invalid weather coordinates")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return result, errors.New("invalid weather timezone")
	}
	unit := "celsius"
	if widget.Units == "fahrenheit" {
		unit = "fahrenheit"
	}
	q := url.Values{"latitude": {strconv.FormatFloat(widget.Latitude, 'f', 6, 64)}, "longitude": {strconv.FormatFloat(widget.Longitude, 'f', 6, 64)}, "timezone": {timezone}, "temperature_unit": {unit}, "current": {"temperature_2m,weather_code"}, "daily": {"temperature_2m_min,temperature_2m_max,precipitation_probability_max,weather_code"}, "forecast_days": {"3"}}
	var response struct {
		Current struct {
			Temperature *float64 `json:"temperature_2m"`
			Code        *int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			Time []string   `json:"time"`
			Min  []*float64 `json:"temperature_2m_min"`
			Max  []*float64 `json:"temperature_2m_max"`
			Rain []*float64 `json:"precipitation_probability_max"`
			Code []*int     `json:"weather_code"`
		} `json:"daily"`
	}
	if err := w.get(ctx, w.ForecastURL, q, &response); err != nil {
		return result, err
	}
	d := response.Daily
	n := len(d.Time)
	if response.Current.Temperature == nil || response.Current.Code == nil || n != 3 || len(d.Min) != n || len(d.Max) != n || len(d.Rain) != n || len(d.Code) != n {
		return result, errors.New("incomplete weather forecast")
	}
	result = model.Weather{Location: widget.Location, Latitude: widget.Latitude, Longitude: widget.Longitude, CurrentTemperature: *response.Current.Temperature, WeatherCode: *response.Current.Code, Daily: []model.WeatherDay{}}
	for i, date := range d.Time {
		if _, err := time.Parse("2006-01-02", date); err != nil || d.Min[i] == nil || d.Max[i] == nil || d.Rain[i] == nil || d.Code[i] == nil {
			return model.Weather{}, errors.New("incomplete daily weather forecast")
		}
		result.Daily = append(result.Daily, model.WeatherDay{Date: date, Min: *d.Min[i], Max: *d.Max[i], RainProbability: *d.Rain[i], WeatherCode: *d.Code[i]})
	}
	return result, nil
}
