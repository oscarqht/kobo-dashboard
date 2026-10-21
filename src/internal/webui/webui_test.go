package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSelfContainedOfflineEditor(t *testing.T) {
	h := Handler()
	for _, tc := range []struct{ path, content string }{{"/", "<title>Paper"}, {"/editor.js", "/api/config"}, {"/style.css", ".device-screen"}} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
		if r.Code != 200 {
			t.Fatalf("%s: status %d", tc.path, r.Code)
		}
		if !strings.Contains(r.Body.String(), tc.content) {
			t.Fatalf("%s missing required content %s", tc.path, tc.content)
		}
		if tc.path == "/" && (strings.Contains(r.Body.String(), "https://") || strings.Contains(r.Body.String(), "http://")) {
			t.Fatal("editor HTML requires a remote resource")
		}
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/missing.js", nil))
	if r.Code != 404 {
		t.Fatal("missing editor asset should return 404")
	}
}
