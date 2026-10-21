package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"kobo-dashboard/src/internal/model"
	"kobo-dashboard/src/internal/providers"
	"kobo-dashboard/src/internal/render"
	"kobo-dashboard/src/internal/webui"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Engine       *Engine
	Demo         bool
	Code         string
	token        string
	hosts        map[string]bool
	mu           sync.Mutex
	lastActivity time.Time
	attempts     []time.Time
	OnAction     func(string)
	Fetch        func()
}

func NewServer(e *Engine, demo bool) (*Server, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return nil, err
	}
	s := &Server{Engine: e, Demo: demo, Code: fmt.Sprintf("%06d", n), token: hex.EncodeToString(b), lastActivity: time.Now(), hosts: map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ip, _, err := net.ParseCIDR(a.String()); err == nil {
			s.hosts[ip.String()] = true
		}
	}
	return s, nil
}
func (s *Server) Expired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.Demo && time.Since(s.lastActivity) > 10*time.Minute
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pair", s.pair)
	mux.HandleFunc("/api/state", s.protected(func(w http.ResponseWriter, r *http.Request) {
		c, snap, v := s.Engine.State()
		v.PairingCode = ""
		v.EditorURL = ""
		jsonResponse(w, map[string]any{"config": c, "snapshot": snap, "view": v, "session": map[string]any{"demo": s.Demo, "editor": true}})
	}))
	mux.HandleFunc("/api/config", s.protected(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		var c model.Config
		if err := decode(r, &c); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := s.Engine.Save(c); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
		go s.refresh()
	}))
	mux.HandleFunc("/api/action", s.protected(s.action))
	mux.HandleFunc("/api/activity", s.protected(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/locations", s.protected(func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 2 || len(q) > 120 {
			http.Error(w, "Enter a location (2–120 characters)", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		locations, err := providers.NewWeather(&http.Client{Timeout: 15 * time.Second}).Search(ctx, q)
		if err != nil {
			http.Error(w, "Location lookup unavailable", 502)
			return
		}
		jsonResponse(w, locations)
	}))
	mux.HandleFunc("/api/preview", s.protected(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		var c model.Config
		if decode(r, &c) != nil {
			http.Error(w, "Invalid configuration", 400)
			return
		}
		if err := c.Validate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		old, snap, v := s.Engine.State()
		v.PairingCode = ""
		v.EditorURL = ""
		// Never label a cached Celsius reading as Fahrenheit or show a previous city.
		for _, widget := range c.Widgets {
			for _, prior := range old.Widgets {
				if prior.ID == widget.ID && widget.Type == "weather" {
					if prior.Location != widget.Location || prior.Latitude != widget.Latitude || prior.Longitude != widget.Longitude {
						delete(snap.Weather, widget.ID)
					} else if prior.Units != widget.Units {
						if data, ok := snap.Weather[widget.ID]; ok {
							convert := func(n float64) float64 {
								if widget.Units == "fahrenheit" {
									return n*9/5 + 32
								}
								return (n - 32) * 5 / 9
							}
							data.CurrentTemperature = convert(data.CurrentTemperature)
							for i := range data.Daily {
								data.Daily[i].Min = convert(data.Daily[i].Min)
								data.Daily[i].Max = convert(data.Daily[i].Max)
							}
							snap.Weather[widget.ID] = data
						}
					}
				}
			}
		}
		b, err := render.Render(c, snap, v)
		if err != nil {
			http.Error(w, "Rendering failed", 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(b)
	}))
	mux.HandleFunc("/api/hits", s.protected(func(w http.ResponseWriter, r *http.Request) {
		c, snap, v := s.Engine.State()
		v.PairingCode = ""
		v.EditorURL = ""
		jsonResponse(w, render.Hits(c, snap, v))
	}))
	mux.HandleFunc("/snapshot.png", s.protected(func(w http.ResponseWriter, r *http.Request) {
		c, snap, v := s.Engine.State()
		v.PairingCode = ""
		v.EditorURL = ""
		b, err := render.Render(c, snap, v)
		if err != nil {
			http.Error(w, "Rendering failed", 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(b)
	}))
	mux.Handle("/", webui.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !s.hosts[host] {
			http.Error(w, "Unrecognized host", 403)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != "http://"+r.Host {
				http.Error(w, "Origin rejected", 403)
				return
			}
			if r.Header.Get("X-Dashboard-Request") != "1" {
				http.Error(w, "Request header required", 403)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Expired() {
			http.Error(w, "Editor session expired. Reopen it on your Kobo.", 401)
			return
		}
		authorized := s.Demo
		if c, err := r.Cookie("dashboard_session"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1 {
			authorized = true
		}
		if !authorized {
			http.Error(w, "Pair with the code shown on your Kobo", 401)
			return
		}
		if r.Method == "POST" {
			s.mu.Lock()
			s.lastActivity = time.Now()
			s.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "dashboard_session", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 600})
		}
		next(w, r)
	}
}
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	if s.Expired() {
		http.Error(w, "Session expired; reopen editor on Kobo", 401)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if decode(r, &body) != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	recent := s.attempts[:0]
	for _, t := range s.attempts {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	s.attempts = recent
	if len(recent) >= 5 {
		http.Error(w, "Too many attempts; wait one minute", 429)
		return
	}
	s.attempts = append(s.attempts, now)
	if subtle.ConstantTimeCompare([]byte(body.Code), []byte(s.Code)) != 1 {
		http.Error(w, "Incorrect pairing code", 401)
		return
	}
	s.lastActivity = now
	s.attempts = nil
	http.SetCookie(w, &http.Cookie{Name: "dashboard_session", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 600})
	jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var a struct {
		Action   string `json:"action"`
		WidgetID string `json:"widget_id"`
		Value    string `json:"value"`
	}
	if decode(r, &a) != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}
	if a.Action == "refresh" {
		go s.refresh()
	} else if a.Action == "editor" {
		if s.OnAction != nil {
			s.OnAction("editor")
		}
	} else if err := s.Engine.Action(a.Action, a.WidgetID, a.Value); err != nil {
		http.Error(w, err.Error(), 400)
		return
	} else if a.Action == "previous" || a.Action == "next" || a.Action == "today" || a.Action == "mode" {
		go s.refresh()
	}
	jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) refresh() {
	if s.Fetch != nil {
		s.Fetch()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = s.Engine.Refresh(ctx)
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	d := json.NewDecoder(io.LimitReader(r.Body, 128*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}
