//go:build linux

package device

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSuspendRefusesUnsafeOrUnsupportedDeviceBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, status, state string
		suspend, rtc        bool
		want                string
	}{
		{"not enabled", "Discharging", "mem", false, true, "suspend not enabled"},
		{"charging", "Charging", "mem", true, true, "Discharging"},
		{"USB fully charged", "Full", "mem", true, true, "Discharging"},
		{"unknown power", "", "mem", true, true, "Discharging"},
		{"missing suspend", "Discharging", "freeze", true, true, "does not advertise"},
		{"missing RTC", "Discharging", "mem", true, false, "suspend prerequisite"},
		{"unsupported RTC", "Discharging", "mem", true, true, "RTC_WKALM_SET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, "sys/class/power_supply/bd71827_bat/status", tc.status)
			fixtureFile(t, root, "sys/power/state", tc.state)
			fixtureFile(t, root, "sys/power/state-extended", "untouched")
			if tc.rtc {
				fixtureFile(t, root, "dev/rtc0", "fake RTC, ioctl must reject")
			}
			d := New(Options{Enabled: true, Root: root, Suspend: tc.suspend, WiFiOff: "/bin/true"})
			err := d.SuspendUntil(context.Background(), time.Now().Add(time.Hour))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			for p, want := range map[string]string{"sys/power/state": tc.state, "sys/power/state-extended": "untouched"} {
				b, err := os.ReadFile(filepath.Join(root, p))
				if err != nil || string(b) != want {
					t.Fatalf("unsafe hardware write to %s: %q %v", p, b, err)
				}
			}
		})
	}
}
func TestWakeAlarmMustBeInFuture(t *testing.T) {
	d := New(Options{Enabled: true, Root: t.TempDir(), Suspend: true})
	if err := d.SuspendUntil(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "five seconds") {
		t.Fatal(err)
	}
}
func TestUnsupportedRTCAndEmptyInputDiscovery(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "dev/input/event0", "not an evdev device")
	d := New(Options{Root: root, Enabled: true})
	if d.options.TouchPath != "" || d.options.PowerPath != "" {
		t.Fatal("regular file falsely detected as hardware")
	}
	if err := d.rtcAlarm(time.Now().Add(time.Hour), true); err == nil {
		t.Fatal("missing RTC accepted")
	}
	fixtureFile(t, root, "dev/rtc0", "not an RTC")
	if err := d.rtcAlarm(time.Now().Add(time.Hour), true); err == nil || !strings.Contains(err.Error(), "RTC_WKALM_SET") {
		t.Fatalf("unsupported RTC: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := d.Events(ctx)
	if ev := <-stream; !strings.Contains(ev.Error, "touch path missing") {
		t.Fatalf("%+v", ev)
	}
	if _, ok := <-stream; ok {
		t.Fatal("input error stream leaked")
	}
}
func TestPrimaryTouchReleaseCoordinates(t *testing.T) {
	root := t.TempDir()
	size := 16
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		size = 24
	}
	var data []byte
	for _, e := range []struct {
		typ, code uint16
		value     int32
	}{{3, 53, 100}, {3, 54, 300}, {3, 57, 2}, {0, 0, 0}, {3, 57, -1}, {0, 0, 0}} {
		b := make([]byte, size)
		binary.LittleEndian.PutUint16(b[size-8:], e.typ)
		binary.LittleEndian.PutUint16(b[size-6:], e.code)
		binary.LittleEndian.PutUint32(b[size-4:], uint32(e.value))
		data = append(data, b...)
	}
	path := fixtureFile(t, root, "touch", string(data))
	d := New(Options{Root: root, Enabled: true, TouchPath: path, TouchMaxX: 1000, TouchMaxY: 1000, Width: 101, Height: 201, MirrorY: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := d.Events(ctx)
	select {
	case ev := <-stream:
		if ev.Action != "tap" || ev.X != 10 || ev.Y != 140 {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("touch input blocked")
	}
	for range stream {
	} // EOF must terminate the stream without leaking goroutines.
}
func TestNativeHelpersFailExplicitly(t *testing.T) {
	d := New(Options{Root: t.TempDir(), Enabled: true})
	if err := d.SetWiFi(context.Background(), true); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatal(err)
	}
	if err := d.ShowPNG("missing"); err == nil || !strings.Contains(err.Error(), "FBInk missing") {
		t.Fatal(err)
	}
	d.options.FBInkPath = "/bin/false"
	if err := d.ShowPNG("missing"); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	image := fixtureFile(t, t.TempDir(), "image.png", "fixture")
	if err := d.ShowPNG(image); err == nil || !strings.Contains(err.Error(), "FBInk display") {
		t.Fatal(err)
	}
	d.options.WiFiOn = "/bin/false"
	if err := d.SetWiFi(context.Background(), true); err == nil || !strings.Contains(err.Error(), "Wi-Fi helper") {
		t.Fatal(err)
	}
}

func TestNativeInputCancellationClosesBlockedReader(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "touch.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	// Keep a writer open so the device read blocks rather than immediately EOF.
	anchor, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.Close()
	ctx, cancel := context.WithCancel(context.Background())
	d := New(Options{Root: root, Enabled: true, TouchPath: path})
	stream := d.Events(ctx)
	cancel()
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("cancellation should close input without a spurious device error")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked input reader leaked after cancellation")
	}
}
