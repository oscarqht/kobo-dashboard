package device

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixtureFile(t *testing.T, root, path, value string) string {
	t.Helper()
	p := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPreviewCannotTouchHardwareEvenWhenConfigured(t *testing.T) {
	root := t.TempDir()
	state := fixtureFile(t, root, "sys/power/state", "original")
	helper := fixtureFile(t, root, "helper.sh", "#!/bin/sh\nprintf changed > '"+state+"'\n")
	if err := os.Chmod(helper, 0700); err != nil {
		t.Fatal(err)
	}
	image := fixtureFile(t, root, "snapshot.png", "image")
	d := New(Options{Root: root, Suspend: true, FBInkPath: helper, WiFiOn: helper, WiFiOff: helper, TouchPath: state})
	calls := []func() error{func() error { return d.ShowPNG(image) }, func() error { return d.SetWiFi(context.Background(), true) }, func() error { return d.SetWiFi(context.Background(), false) }, func() error { return d.SuspendUntil(context.Background(), time.Now().Add(time.Hour)) }}
	for _, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "--device") {
			t.Fatalf("expected explicit opt-in error, got %v", err)
		}
	}
	event := <-d.Events(context.Background())
	if !strings.Contains(event.Error, "--device") {
		t.Fatalf("events: %+v", event)
	}
	got, err := os.ReadFile(state)
	if err != nil || string(got) != "original" {
		t.Fatalf("preview modified hardware fixture: %q %v", got, err)
	}
}
func TestNonLinuxRejectsNativeAccess(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("non-Linux safety boundary")
	}
	d := New(Options{Root: t.TempDir(), Enabled: true, Suspend: true})
	if err := d.ShowPNG("missing"); err == nil || !strings.Contains(err.Error(), "requires Linux") {
		t.Fatal(err)
	}
	if err := d.SetWiFi(context.Background(), true); err == nil || !strings.Contains(err.Error(), "requires Linux") {
		t.Fatal(err)
	}
	if err := d.SuspendUntil(context.Background(), time.Now().Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "requires Linux") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := d.Events(ctx)
	select {
	case ev := <-stream:
		if !strings.Contains(ev.Error, "requires Linux") {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled input stream did not return")
	}
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("input stream left open")
		}
	case <-time.After(time.Second):
		t.Fatal("input stream leaked")
	}
	if err := d.rtcAlarm(time.Now(), true); err == nil {
		t.Fatal("unsupported RTC accepted")
	}
}
func TestMalformedInputDoesNotProduceEvents(t *testing.T) {
	if _, _, _, err := ParseInput(bytes.NewReader(nil), 20); err == nil {
		t.Fatal("unknown ABI accepted")
	}
	for _, size := range []int{16, 24} {
		for _, length := range []int{0, 1, size - 1} {
			_, _, _, err := ParseInput(bytes.NewReader(make([]byte, length)), size)
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("size %d length %d: %v", size, length, err)
			}
		}
	}
}
func TestDoctorReportsResolvedPathsAndTLSWithoutMutations(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SSL_CERT_FILE", "/bundle/config/cert.pem")
	files := map[string]string{"dev/fb0": "framebuffer marker", "dev/rtc0": "RTC marker", "lib/ld-linux-armhf.so.3": "loader marker", "sys/power/state-extended": "0", "sys/class/graphics/fb0/virtual_size": "1404,1872\n", "sys/class/rtc/rtc0/name": "mtk-rtc\n", "sys/class/input/event2/device/name": "Touch Panel\n"}
	for p, v := range files {
		fixtureFile(t, root, p, v)
	}
	fbink := fixtureFile(t, root, "fbink", "binary marker")
	d := New(Options{Root: root, FBInkPath: fbink, TouchPath: "/dev/input/event2", PowerPath: "/dev/input/event0", TouchMaxX: 4095, TouchMaxY: 8191, WiFiOn: "/bundle/config/wifi-on.sh", WiFiOff: "/bundle/config/wifi-off.sh"})
	r := d.Doctor()
	if r.TLSCA != "/bundle/config/cert.pem" || r.TouchPath != "/dev/input/event2" || r.PowerPath != "/dev/input/event0" || r.TouchMaxX != 4095 || r.TouchMaxY != 8191 || r.Framebuffer != "1404,1872" || r.RTC != "mtk-rtc" {
		t.Fatalf("incomplete diagnostics: %+v", r)
	}
	if !reflect.DeepEqual(r.Inputs, []string{"event2: Touch Panel"}) || len(r.Missing) != 0 {
		t.Fatalf("incorrect discovered prerequisites: %+v", r)
	}
	for p, v := range files {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil || string(b) != v {
			t.Fatalf("doctor changed %s: %q %v", p, b, err)
		}
	}
}
