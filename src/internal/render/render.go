// Package render draws the native e-ink interface without a browser runtime.
package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"kobo-dashboard/src/internal/model"
)

const Width, Height = 1404, 1872

var ink = color.Gray{Y: 26}
var gray = color.Gray{Y: 105}
var rule = color.Gray{Y: 200}
var regular, _ = opentype.Parse(goregular.TTF)
var bold, _ = opentype.Parse(gobold.TTF)

type canvas struct {
	img   *image.RGBA
	faces map[string]font.Face
	hits  []model.Hit
	loc   *time.Location
}

func newCanvas() *canvas {
	c := &canvas{img: image.NewRGBA(image.Rect(0, 0, Width, Height)), faces: map[string]font.Face{}}
	draw.Draw(c.img, c.img.Bounds(), image.White, image.Point{}, draw.Src)
	return c
}
func (c *canvas) face(size int, weight bool) font.Face {
	key := fmt.Sprintf("%d:%t", size, weight)
	if f := c.faces[key]; f != nil {
		return f
	}
	ttf := regular
	if weight {
		ttf = bold
	}
	f, _ := opentype.NewFace(ttf, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	c.faces[key] = f
	return f
}
func (c *canvas) close() {
	for _, f := range c.faces {
		f.Close()
	}
}
func (c *canvas) text(s string, x, y, size int, weight bool, col color.Color) {
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: c.face(size, weight), Dot: fixed.P(x, y)}
	d.DrawString(s)
}
func (c *canvas) measure(s string, size int, weight bool) int {
	return font.MeasureString(c.face(size, weight), s).Ceil()
}
func (c *canvas) fit(s string, width, size int, weight bool) string {
	s = strings.Join(strings.Fields(s), " ")
	if c.measure(s, size, weight) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if c.measure(string(r)+"…", size, weight) <= width {
			return string(r) + "…"
		}
	}
	return ""
}
func (c *canvas) line(x, y, w int, col color.Color) { c.fill(image.Rect(x, y, x+w, y+1), col) }
func (c *canvas) fill(r image.Rectangle, col color.Color) {
	draw.Draw(c.img, r, image.NewUniform(col), image.Point{}, draw.Src)
}
func widgetRect(w model.Widget) image.Rectangle {
	// Leave a fixed, readable header and footer around the 12 × 12 layout.
	const left, top, unitW, unitH = 54, 150, 108, 136
	return image.Rect(left+w.X*unitW, top+w.Y*unitH, left+(w.X+w.W)*unitW-16, top+(w.Y+w.H)*unitH-20)
}
func nowFor(config model.Config, view model.View) time.Time {
	now := view.Now
	if now.IsZero() {
		now = time.Now()
	}
	loc, err := time.LoadLocation(config.Timezone)
	if err == nil {
		now = now.In(loc)
	}
	return now
}

// Render generates the same PNG used by the framebuffer backend and editor.
func Render(config model.Config, snapshot model.Snapshot, view model.View) ([]byte, error) {
	c := build(config, snapshot, view)
	defer c.close()
	var out bytes.Buffer
	err := png.Encode(&out, c.img)
	return out.Bytes(), err
}

// Hits returns touch targets in native 1404 × 1872 coordinates.
func Hits(config model.Config, snapshot model.Snapshot, view model.View) []model.Hit {
	c := build(config, snapshot, view)
	defer c.close()
	return c.hits
}
func build(config model.Config, snapshot model.Snapshot, view model.View) *canvas {
	c := newCanvas()
	now := nowFor(config, view)
	c.loc = now.Location()
	c.text("PAPER", 54, 65, 23, true, ink)
	c.text(strings.ToUpper(now.Format("Monday, 2 January 2006")), 54, 109, 21, false, gray)
	c.line(54, 128, 1296, ink)
	for _, w := range config.Widgets {
		r := widgetRect(w)
		if w.Type == "weather" {
			c.weather(w, snapshot, r)
		} else {
			c.calendar(w, snapshot, view, now, r)
		}
	}
	c.line(54, 1808, 1296, rule)
	if snapshot.Demo {
		c.text("SAMPLE DATA · Set up Google Calendar and your location", 54, 1844, 19, false, gray)
	} else {
		c.text("Hourly updates · Press power to interact", 54, 1844, 19, false, gray)
	}
	c.text("Weather: Open-Meteo · Location search: GeoNames", 54, 1869, 14, false, gray)
	for i, b := range []struct{ label, action string }{{"Refresh", "refresh"}, {"Layout", "editor"}, {"Read", "reader"}} {
		x := 970 + i*130
		c.text(b.label, x, 1844, 20, true, ink)
		c.hits = append(c.hits, model.Hit{X: x - 12, Y: 1818, W: 120, H: 48, Action: b.action})
	}
	if view.Day != "" {
		c.dayOverlay(snapshot, view, config)
	}
	if view.PairingCode != "" {
		c.editorOverlay(view)
	}
	return c
}
func (c *canvas) status(s model.Status, x, y, w int) {
	label := "Not yet updated"
	if !s.UpdatedAt.IsZero() {
		label = "Updated " + s.UpdatedAt.In(c.loc).Format("15:04")
	}
	if s.Error != "" {
		label = "STALE · " + label
	}
	c.text(c.fit(label, w, 18, false), x, y, 18, false, gray)
}
func condition(code int) string {
	switch {
	case code == 0:
		return "Clear skies"
	case code <= 3:
		return "Partly cloudy"
	case code <= 48:
		return "Foggy"
	case code <= 67:
		return "Rain"
	case code <= 77:
		return "Snow"
	case code <= 82:
		return "Rain showers"
	case code <= 86:
		return "Snow showers"
	default:
		return "Thunderstorms"
	}
}
func (c *canvas) weather(w model.Widget, s model.Snapshot, r image.Rectangle) {
	x, y, ww := r.Min.X, r.Min.Y, r.Dx()
	data, ok := s.Weather[w.ID]
	name := w.Location
	if data.Location != "" {
		name = data.Location
	}
	if name == "" {
		name = "Your weather"
	}
	c.text(c.fit(strings.ToUpper(name), ww-20, 22, true), x, y+30, 22, true, ink)
	if !ok {
		c.text("Choose your location", x, y+100, 30, false, ink)
		c.text("Open Layout to get started", x, y+142, 22, false, gray)
		c.status(s.Status[model.WeatherStatusID(w.ID)], x, r.Max.Y-16, ww)
		return
	}
	unit := "°C"
	if w.Units == "fahrenheit" {
		unit = "°F"
	}
	size := 96
	if ww < 700 || r.Dy() < 300 {
		size = 64
	}
	c.text(fmt.Sprintf("%.0f%s", data.CurrentTemperature, unit), x, y+size+54, size, false, ink)
	c.text(c.fit(condition(data.WeatherCode), ww, 25, false), x, y+size+91, 25, false, ink)
	if len(data.Daily) > 0 {
		d := data.Daily[0]
		c.text(c.fit(fmt.Sprintf("H %.0f°  /  L %.0f°   ·   Rain %.0f%%", d.Max, d.Min, d.RainProbability), ww, 21, false), x, y+size+116, 21, false, gray)
	}
	if ww > 750 && len(data.Daily) > 0 {
		fx := x + ww/2
		fw := (ww - ww/2) / 3
		for i, d := range data.Daily {
			if i >= 3 {
				break
			}
			dx := fx + i*fw
			label := d.Date
			if date, e := time.Parse("2006-01-02", d.Date); e == nil {
				label = strings.ToUpper(date.Format("Mon"))
			}
			c.text(label, dx, y+76, 21, true, gray)
			c.text(fmt.Sprintf("%.0f°", d.Max), dx, y+132, 40, false, ink)
			c.text(fmt.Sprintf("%.0f° / %.0f%% rain", d.Min, d.RainProbability), dx, y+172, 18, false, gray)
		}
	} else if len(data.Daily) > 0 {
		var forecast []string
		for i, d := range data.Daily {
			if i >= 3 {
				break
			}
			date, _ := time.Parse("2006-01-02", d.Date)
			forecast = append(forecast, fmt.Sprintf("%s %.0f°", date.Format("Mon"), d.Max))
		}
		c.text(c.fit(strings.Join(forecast, "   "), ww, 17, false), x, r.Max.Y-48, 17, false, gray)
	}
	c.status(s.Status[model.WeatherStatusID(w.ID)], x, r.Max.Y-15, ww)
	c.line(x, r.Max.Y, ww, rule)
}

func (c *canvas) editorOverlay(v model.View) {
	c.fill(image.Rect(74, 420, 1330, 1300), ink)
	c.fill(image.Rect(77, 423, 1327, 1297), color.White)
	c.text("Make it your own.", 130, 520, 50, true, ink)
	c.text("On your phone or computer, open:", 130, 610, 30, false, gray)
	c.text(c.fit(v.EditorURL, 1120, 32, true), 130, 674, 32, true, ink)
	c.text("Enter this pairing code", 130, 775, 28, false, gray)
	c.text(v.PairingCode, 130, 890, 82, true, ink)
	c.text("Use the same Wi-Fi network as your Kobo.", 130, 1005, 27, false, gray)
	c.text("Editor closes after 10 minutes without activity.", 130, 1050, 25, false, gray)
	c.text("Close ×", 1100, 1210, 29, true, ink)
	c.hits = []model.Hit{{X: 1040, Y: 1140, W: 240, H: 110, Action: "close"}}
}
