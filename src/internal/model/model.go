package model

import (
	"errors"
	"fmt"
	"time"
)

type Config struct {
	Version  int      `json:"version"`
	Timezone string   `json:"timezone"`
	Language string   `json:"language"`
	Widgets  []Widget `json:"widgets"`
}
type Widget struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	X           int      `json:"x"`
	Y           int      `json:"y"`
	W           int      `json:"w"`
	H           int      `json:"h"`
	Mode        string   `json:"mode,omitempty"`
	CalendarIDs []string `json:"calendar_ids,omitempty"`
	Location    string   `json:"location,omitempty"`
	Latitude    float64  `json:"latitude,omitempty"`
	Longitude   float64  `json:"longitude,omitempty"`
	Units       string   `json:"units,omitempty"`
}
type Calendar struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}
type Event struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendar_id"`
	Title      string `json:"title"`
	Start      string `json:"start"`
	End        string `json:"end"`
	AllDay     bool   `json:"all_day"`
}
type WeatherDay struct {
	Date            string  `json:"date"`
	Min             float64 `json:"min"`
	Max             float64 `json:"max"`
	RainProbability float64 `json:"rain_probability"`
	WeatherCode     int     `json:"weather_code"`
}
type Weather struct {
	Location           string       `json:"location"`
	Latitude           float64      `json:"latitude"`
	Longitude          float64      `json:"longitude"`
	CurrentTemperature float64      `json:"current_temperature"`
	WeatherCode        int          `json:"weather_code"`
	Daily              []WeatherDay `json:"daily"`
}
type Status struct {
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
}
type Snapshot struct {
	Calendars []Calendar         `json:"calendars"`
	Events    []Event            `json:"events"`
	Weather   map[string]Weather `json:"weather"`
	Status    map[string]Status  `json:"status"`
	Demo      bool               `json:"demo"`
}
type View struct {
	DayPage     int            `json:"day_page,omitempty"`
	PairingCode string         `json:"pairing_code,omitempty"`
	EditorURL   string         `json:"editor_url,omitempty"`
	Offsets     map[string]int `json:"offsets"`
	Day         string         `json:"day,omitempty"`
	WidgetID    string         `json:"widget_id,omitempty"`
	Now         time.Time      `json:"now"`
}
type Hit struct {
	X, Y, W, H int
	Action     string
	WidgetID   string
	Value      string
}

func DefaultConfig() Config {
	return Config{Version: 1, Timezone: "Asia/Singapore", Language: "en", Widgets: []Widget{
		{ID: "weather-1", Type: "weather", X: 0, Y: 0, W: 12, H: 3, Location: "", Units: "celsius"},
		{ID: "calendar-1", Type: "calendar", X: 0, Y: 3, W: 12, H: 9, Mode: "month", CalendarIDs: []string{"primary"}},
	}}
}
func (c Config) Validate() error {
	if c.Version != 1 {
		return errors.New("unsupported configuration version")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return errors.New("choose a valid IANA timezone")
	}
	if c.Language != "en" {
		return errors.New("version one supports English")
	}
	if len(c.Widgets) < 1 || len(c.Widgets) > 16 {
		return errors.New("choose between 1 and 16 widgets")
	}
	ids := map[string]bool{}
	for i, w := range c.Widgets {
		if w.ID == "" || len(w.ID) > 80 || ids[w.ID] {
			return errors.New("widget IDs must be unique and nonempty")
		}
		ids[w.ID] = true
		if w.X < 0 || w.Y < 0 || w.W < 1 || w.H < 1 || w.X+w.W > 12 || w.Y+w.H > 12 {
			return fmt.Errorf("widget %s must fit the 12 by 12 grid", w.ID)
		}
		if w.Type != "weather" && w.Type != "calendar" {
			return errors.New("unknown widget type")
		}
		if w.Type == "calendar" && (w.Mode != "month" && w.Mode != "week") {
			return errors.New("calendar mode must be month or week")
		}
		if w.Type == "calendar" && (w.W < 6 || w.H < 4) {
			return errors.New("calendar needs at least 6 columns and 4 rows")
		}
		if w.Type == "weather" && (w.W < 4 || w.H < 2) {
			return errors.New("weather needs at least 4 columns and 2 rows")
		}
		if w.Units != "" && w.Units != "celsius" && w.Units != "fahrenheit" {
			return errors.New("invalid temperature units")
		}
		if w.Latitude < -90 || w.Latitude > 90 || w.Longitude < -180 || w.Longitude > 180 {
			return errors.New("invalid weather coordinates")
		}
		if len(w.Location) > 200 || len(w.CalendarIDs) > 50 {
			return errors.New("widget settings too large")
		}
		for _, p := range c.Widgets[:i] {
			if w.X < p.X+p.W && w.X+w.W > p.X && w.Y < p.Y+p.H && w.Y+w.H > p.Y {
				return errors.New("widgets must not overlap")
			}
		}
	}
	return nil
}

func CalendarStatusID(id string) string { return "calendar:" + id }
func WeatherStatusID(id string) string  { return "weather:" + id }
