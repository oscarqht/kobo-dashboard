package main

import (
	"context"
	"errors"
	"kobo-dashboard/src/internal/app"
	"kobo-dashboard/src/internal/device"
	"kobo-dashboard/src/internal/render"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeDevice struct {
	report   device.Report
	events   chan device.Event
	wifi     []bool
	shown    int
	failWiFi bool
	failShow bool
}

func (f *fakeDevice) Doctor() device.Report                      { return f.report }
func (f *fakeDevice) Events(context.Context) <-chan device.Event { return f.events }
func (f *fakeDevice) ShowPNG(path string) error {
	if f.failShow {
		return errors.New("display failed")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(b) < 8 || string(b[1:4]) != "PNG" {
		return errors.New("not a PNG")
	}
	f.shown++
	return nil
}
func (f *fakeDevice) SetWiFi(_ context.Context, on bool) error {
	f.wifi = append(f.wifi, on)
	if on && f.failWiFi {
		return errors.New("association failed")
	}
	return nil
}
func (f *fakeDevice) SuspendUntil(context.Context, time.Time) error {
	return errors.New("not a real device")
}
func newFake() *fakeDevice {
	return &fakeDevice{report: device.Report{Runtime: "linux/arm", Framebuffer: "1404,1872", TouchPath: "fake"}, events: make(chan device.Event, 8)}
}
func TestNativeModeThenReturnToReader(t *testing.T) {
	e, err := app.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	f := newFake()
	c, s, v := e.State()
	hits := render.Hits(c, s, v)
	for _, target := range []struct{ action, value string }{{"mode", "week"}, {"reader", ""}} {
		found := false
		for _, h := range hits {
			if h.Action == target.action && h.Value == target.value {
				f.events <- device.Event{Action: "tap", X: h.X + h.W/2, Y: h.Y + h.H/2}
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing touch target", target)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runDevice(ctx, e, f, "127.0.0.1:8787", e.Dir, false); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("native loop timed out rather than returning to reader")
	}
	c, _, _ = e.State()
	if c.Widgets[1].Mode != "week" {
		t.Fatal("native mode did not persist")
	}
	if f.shown < 2 {
		t.Fatal("native PNG not presented")
	}
	if len(f.wifi) < 2 || !f.wifi[0] || f.wifi[1] {
		t.Fatalf("radio not shut down after fetch: %v", f.wifi)
	}
}
func TestInputFailureExitsAndNetworkFailureCleansUp(t *testing.T) {
	for _, networkFailure := range []bool{false, true} {
		e, err := app.New(t.TempDir(), false)
		if err != nil {
			t.Fatal(err)
		}
		f := newFake()
		f.failWiFi = networkFailure
		f.events <- device.Event{Error: "input device removed"}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = runDevice(ctx, e, f, "127.0.0.1:8787", e.Dir, false)
		cancel()
		if err == nil || !strings.Contains(err.Error(), "input unavailable") {
			t.Fatal(err)
		}
		if len(f.wifi) < 2 || f.wifi[len(f.wifi)-1] {
			t.Fatal("radio cleanup missing", f.wifi)
		}
	}
}
func TestNativePrerequisitesRefused(t *testing.T) {
	for _, report := range []device.Report{{Runtime: "darwin/arm64"}, {Runtime: "linux/arm"}, {Runtime: "linux/arm", Framebuffer: "1404,1872"}} {
		e, _ := app.New(t.TempDir(), false)
		f := newFake()
		f.report = report
		if err := runDevice(context.Background(), e, f, "127.0.0.1:8787", e.Dir, false); err == nil {
			t.Fatal("missing prerequisite accepted")
		}
		if len(f.wifi) > 0 || f.shown > 0 {
			t.Fatal("hardware used before validation")
		}
	}
	e, _ := app.New(t.TempDir(), false)
	f := newFake()
	f.failShow = true
	if err := runDevice(context.Background(), e, f, "127.0.0.1:8787", e.Dir, false); err == nil {
		t.Fatal("display failure ignored")
	}
	if len(f.wifi) > 0 {
		t.Fatal("network started before display validation")
	}
}
