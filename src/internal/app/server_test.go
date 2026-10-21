package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	e, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(e, false)
	if err != nil {
		t.Fatal(err)
	}
	return s, s.Handler()
}
func request(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
	r.Header.Set("X-Dashboard-Request", "1")
	r.Header.Set("Origin", "http://127.0.0.1:8787")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func pair(t *testing.T, s *Server, h http.Handler) *http.Cookie {
	t.Helper()
	w := request(h, "POST", "/api/pair", `{"code":"`+s.Code+`"}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	return cs[0]
}
func TestPairProtectsPrivateState(t *testing.T) {
	s, h := testServer(t)
	for _, path := range []string{"/api/state", "/snapshot.png", "/api/locations?q=London"} {
		w := request(h, "GET", path, "", nil)
		if w.Code != 401 {
			t.Fatalf("%s status %d", path, w.Code)
		}
	}
	cookie := pair(t, s, h)
	w := request(h, "GET", "/api/state", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var state map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"refresh_token", "client_secret", "credentials"} {
		if strings.Contains(w.Body.String(), term) {
			t.Fatalf("exposed %s", term)
		}
	}
}
func TestRejectCrossOriginAndDNSRebinding(t *testing.T) {
	s, h := testServer(t)
	cookie := pair(t, s, h)
	for _, test := range []string{"origin", "host", "header"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/activity", strings.NewReader("{}"))
		r.Header.Set("X-Dashboard-Request", "1")
		r.Header.Set("Origin", "http://127.0.0.1:8787")
		r.AddCookie(cookie)
		switch test {
		case "origin":
			r.Header.Set("Origin", "http://evil.example")
		case "host":
			r.Host = "evil.example"
		case "header":
			r.Header.Del("X-Dashboard-Request")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s accepted: %d", test, w.Code)
		}
	}
}
func TestPairThrottlingAndExpiration(t *testing.T) {
	s, h := testServer(t)
	for i := 0; i < 5; i++ {
		w := request(h, "POST", "/api/pair", `{"code":"incorrect"}`, nil)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request(h, "POST", "/api/pair", `{"code":"`+s.Code+`"}`, nil); w.Code != 429 {
		t.Fatal("pair rate limit missing")
	}
	s.attempts = nil
	cookie := pair(t, s, h)
	s.lastActivity = time.Now().Add(-11 * time.Minute)
	if w := request(h, "GET", "/api/state", "", cookie); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
}
func TestPollingDoesNotExtendSession(t *testing.T) {
	s, h := testServer(t)
	cookie := pair(t, s, h)
	last := time.Now().Add(-time.Minute)
	s.lastActivity = last
	if w := request(h, "GET", "/api/state", "", cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if !s.lastActivity.Equal(last) {
		t.Fatal("poll kept device awake")
	}
	if w := request(h, "POST", "/api/activity", "{}", cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if !s.lastActivity.After(last) {
		t.Fatal("user activity did not extend session")
	}
}
func TestMalformedConfigCannotReplaceLayout(t *testing.T) {
	s, h := testServer(t)
	cookie := pair(t, s, h)
	for _, body := range []string{`{}`, `{"version":1} {}`, `{"unexpected":true}`} {
		w := request(h, "POST", "/api/config", body, cookie)
		if w.Code != 400 {
			t.Fatal("accepted malformed config", body)
		}
	}
	c, _, _ := s.Engine.State()
	if len(c.Widgets) != 2 {
		t.Fatal("config mutated")
	}
}

func TestPreviewIsPrivateAndDoesNotSaveDraft(t *testing.T) {
	s, h := testServer(t)
	cookie := pair(t, s, h)
	s.Engine.SetEditor(s.Code, "http://192.168.1.2:8787")
	w := request(h, "GET", "/api/hits", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if strings.Count(w.Body.String(), `"Action":"day"`) != 35 {
		t.Fatal("editor overlay leaked into browser preview", w.Body.String())
	}
	c, _, _ := s.Engine.State()
	c.Widgets[1].Mode = "week"
	body, _ := json.Marshal(c)
	w = request(h, "POST", "/api/preview", string(body), cookie)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, _, _ := s.Engine.State()
	if saved.Widgets[1].Mode != "month" {
		t.Fatal("preview saved draft")
	}
	if w := request(h, "POST", "/api/preview", `{}`, cookie); w.Code != 400 {
		t.Fatal("invalid draft accepted")
	}
	if w := request(h, "GET", "/api/preview", "", cookie); w.Code != 405 {
		t.Fatal("preview method unchecked")
	}
}
func TestActionAndConfigRoundTrip(t *testing.T) {
	s, h := testServer(t)
	cookie := pair(t, s, h)
	fetched := make(chan struct{}, 8)
	s.Fetch = func() { fetched <- struct{}{} }
	for _, body := range []string{`{"action":"mode","widget_id":"calendar-1","value":"week"}`, `{"action":"next","widget_id":"calendar-1"}`, `{"action":"today","widget_id":"calendar-1"}`, `{"action":"refresh"}`} {
		w := request(h, "POST", "/api/action", body, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	c, _, _ := s.Engine.State()
	if c.Widgets[1].Mode != "week" {
		t.Fatal("mode not applied")
	}
	body, _ := json.Marshal(c)
	w := request(h, "POST", "/api/config", string(body), cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("refresh not dispatched")
	}
	for _, body := range []string{`{"action":"mode","widget_id":"missing","value":"week"}`, `{"action":"unknown"}`, `{`} {
		if w := request(h, "POST", "/api/action", body, cookie); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	for _, path := range []string{"/api/config", "/api/action", "/api/activity", "/api/pair"} {
		if w := request(h, "GET", path, "", cookie); w.Code != 405 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/", "/editor.js", "/style.css"} {
		if w := request(h, "GET", path, "", nil); w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
}
func TestDemoRefreshAndViewNavigation(t *testing.T) {
	e, err := New(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	c, s, _ := e.State()
	if !s.Demo || len(s.Events) == 0 || len(s.Weather) == 0 {
		t.Fatal("demo empty")
	}
	if err := e.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, after, _ := e.State()
	if len(after.Events) != len(s.Events) {
		t.Fatal("sample events duplicated")
	}
	id := c.Widgets[1].ID
	for _, a := range []struct{ action, value string }{{"day", "2026-10-21"}, {"day-next", ""}, {"day-previous", ""}, {"close", ""}, {"previous", ""}, {"today", ""}} {
		if err := e.Action(a.action, id, a.value); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []struct{ action, id, value string }{{"day", id, "invalid"}, {"day", "missing", "2026-10-21"}, {"next", "missing", ""}, {"day-next", id, ""}, {"bad", id, ""}} {
		if e.Action(a.action, a.id, a.value) == nil {
			t.Fatal("bad action accepted", a)
		}
	}
	if err := e.Action("reader", "", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-e.Exit:
	default:
		t.Fatal("reader exit not signaled")
	}
}
