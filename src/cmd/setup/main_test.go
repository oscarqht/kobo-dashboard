package main

import (
	"flag"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallbackRejectsMissingForgedAndRepeatedState(t *testing.T) {
	result := make(chan string, 1)
	handler := callback("expected", result)
	for _, query := range []string{"?code=secret", "?state=wrong&code=secret", "?state=expected&state=wrong&code=secret"} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/callback"+query, nil))
		if w.Code != 400 {
			t.Fatalf("accepted %s", query)
		}
	}
	if len(result) != 0 {
		t.Fatal("untrusted callback consumed authorization")
	}
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "/callback?state=expected&code=good", nil))
	if w.Code != 200 || <-result != "good" {
		t.Fatal("valid callback failed")
	}
}

func TestCallbackRejectsWrongMethodMissingCodeAndDuplicates(t *testing.T) {
	result := make(chan string, 1)
	handler := callback("expected", result)
	for _, tc := range []struct {
		method, target string
		want           int
	}{
		{"POST", "/callback?state=expected&code=secret", 405},
		{"GET", "/callback?state=expected", 400},
		{"GET", "/callback?state=expected&code=a&code=b", 400},
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(tc.method, tc.target, nil))
		if w.Code != tc.want {
			t.Fatal(tc, w.Code)
		}
		if len(result) != 0 {
			t.Fatal("invalid callback delivered a result")
		}
	}
	result <- "already-completed"
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "/callback?state=expected&code=duplicate", nil))
	if w.Code != 409 {
		t.Fatal("duplicate callback accepted", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") != "default-src 'none'" {
		t.Fatal("callback can be cached or execute content")
	}
}

func TestCallbackDenialIsReportedWithoutProviderDetails(t *testing.T) {
	result := make(chan string, 1)
	handler := callback("expected", result)
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "/callback?state=expected&error=access_denied&error_description=private", nil))
	if w.Code != 200 || <-result != "" {
		t.Fatal("denial not delivered")
	}
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("provider detail reflected in callback")
	}
}

func TestSetupRejectsInvalidInputBeforeBrowserOrServer(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.json")
	os.WriteFile(malformed, []byte("{"), 0600)
	webClient := filepath.Join(dir, "web.json")
	os.WriteFile(webClient, []byte(`{"web":{"client_id":"wrong-type"}}`), 0600)
	desktop := filepath.Join(dir, "desktop.json")
	os.WriteFile(desktop, []byte(`{"installed":{"client_id":"desktop-client"}}`), 0600)
	exists := filepath.Join(dir, "existing-output.json")
	os.WriteFile(exists, []byte("preserve-existing-private-token"), 0600)
	for _, args := range [][]string{
		{},
		{"-client", filepath.Join(dir, "missing.json")},
		{"-client", malformed},
		{"-client", webClient},
		{"-client", desktop, "-output", exists},
	} {
		previousFlags, previousArgs := flag.CommandLine, os.Args
		flag.CommandLine = flag.NewFlagSet("setup-test", flag.ContinueOnError)
		os.Args = append([]string{"setup-test"}, args...)
		err := run()
		flag.CommandLine, os.Args = previousFlags, previousArgs
		if err == nil {
			t.Fatalf("accepted invalid setup arguments %v", args)
		}
	}
	data, err := os.ReadFile(exists)
	if err != nil || string(data) != "preserve-existing-private-token" {
		t.Fatal("setup overwrote existing credentials", err)
	}
}
