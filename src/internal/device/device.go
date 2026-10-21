// Package device keeps all Kobo hardware access behind explicit opt-in.
package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Options struct {
	Enabled bool
	// Root permits read-only discovery against a captured sysfs tree in tests.
	Root                 string
	FBInkPath            string
	TouchPath            string
	PowerPath            string
	WiFiOn               string
	WiFiOff              string
	Width, Height        int
	TouchMaxX, TouchMaxY int
	MirrorY              bool
	// Suspend is separately opt-in until the exact firmware passes hardware validation.
	Suspend bool
}
type Device struct{ options Options }
type Event struct {
	Action string
	X, Y   int
	Error  string
}
type Report struct {
	TLSCA       string   `json:"tls_ca_bundle"`
	Runtime     string   `json:"runtime"`
	Enabled     bool     `json:"enabled"`
	Firmware    string   `json:"firmware"`
	Framebuffer string   `json:"framebuffer"`
	Battery     string   `json:"battery"`
	Capacity    string   `json:"capacity"`
	Status      string   `json:"status"`
	RTC         string   `json:"rtc"`
	Inputs      []string `json:"inputs"`
	TouchPath   string   `json:"touch_path"`
	PowerPath   string   `json:"power_path"`
	TouchMaxX   int      `json:"touch_max_x"`
	TouchMaxY   int      `json:"touch_max_y"`
	Missing     []string `json:"missing"`
}

func New(o Options) *Device {
	if o.Root == "" {
		o.Root = "/"
	}
	if o.Width == 0 {
		o.Width = 1404
	}
	if o.Height == 0 {
		o.Height = 1872
	}
	d := &Device{options: o}
	d.discoverInputs()
	return d
}
func (d *Device) path(p string) string { return filepath.Join(d.options.Root, p) }
func (d *Device) read(p string) string {
	b, _ := os.ReadFile(d.path(p))
	return strings.TrimSpace(string(b))
}
func (d *Device) guard() error {
	if !d.options.Enabled {
		return errors.New("hardware disabled: launch explicitly with --device")
	}
	if runtime.GOOS != "linux" {
		return errors.New("Kobo hardware requires Linux")
	}
	return nil
}
func (d *Device) Doctor() Report {
	r := Report{Runtime: runtime.GOOS + "/" + runtime.GOARCH, Enabled: d.options.Enabled, Firmware: d.read("/mnt/onboard/.kobo/version"), Framebuffer: d.read("/sys/class/graphics/fb0/virtual_size")}
	r.TLSCA = os.Getenv("SSL_CERT_FILE")
	r.TouchPath = d.options.TouchPath
	r.PowerPath = d.options.PowerPath
	r.TouchMaxX = d.options.TouchMaxX
	r.TouchMaxY = d.options.TouchMaxY
	r.Battery = "/sys/class/power_supply/bd71827_bat"
	r.Capacity = d.read(r.Battery + "/capacity")
	r.Status = d.read(r.Battery + "/status")
	r.RTC = d.read("/sys/class/rtc/rtc0/name")
	inputs, _ := filepath.Glob(d.path("/sys/class/input/event*/device/name"))
	for _, p := range inputs {
		b, _ := os.ReadFile(p)
		r.Inputs = append(r.Inputs, filepath.Base(filepath.Dir(filepath.Dir(p)))+": "+strings.TrimSpace(string(b)))
	}
	for _, p := range []string{"/dev/fb0", "/lib/ld-linux-armhf.so.3", "/dev/rtc0", "/sys/power/state-extended"} {
		if _, err := os.Stat(d.path(p)); err != nil {
			r.Missing = append(r.Missing, p)
		}
	}
	if d.options.FBInkPath == "" {
		r.Missing = append(r.Missing, "FBInk executable path")
	} else if _, err := os.Stat(d.options.FBInkPath); err != nil {
		r.Missing = append(r.Missing, "FBInk executable: "+err.Error())
	}
	if d.options.TouchPath == "" {
		r.Missing = append(r.Missing, "calibrated touch input path")
	}
	if d.options.WiFiOn == "" || d.options.WiFiOff == "" {
		r.Missing = append(r.Missing, "validated Wi-Fi on/off helpers")
	}
	return r
}
func (d *Device) ShowPNG(path string) error {
	if err := d.guard(); err != nil {
		return err
	}
	if d.options.FBInkPath == "" {
		return errors.New("FBInk missing: set --fbink to a Kobo binary with PNG support")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err = os.Stat(abs); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// --img takes a raw path, avoiding the comma-delimited --image parser.
	out, err := exec.CommandContext(ctx, d.options.FBInkPath, "--quiet", "--clear", "--flash", "--img", abs).CombinedOutput()
	if err != nil {
		return fmt.Errorf("FBInk display: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
func (d *Device) SetWiFi(ctx context.Context, on bool) error {
	if err := d.guard(); err != nil {
		return err
	}
	path := d.options.WiFiOff
	if on {
		path = d.options.WiFiOn
	}
	if path == "" {
		return errors.New("Wi-Fi helper not configured; validate firmware-specific helper before battery operation")
	}
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(c, path).CombinedOutput(); err != nil {
		return fmt.Errorf("Wi-Fi helper: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
func (d *Device) write(p, s string) error { return os.WriteFile(d.path(p), []byte(s), 0600) }

// SuspendUntil uses the Linux RTC_WKALM_SET/RD ioctl ABI. MediaTek firmware must first be
// validated on-device; in particular, never suspend while connected to USB power.
func (d *Device) SuspendUntil(ctx context.Context, until time.Time) error {
	if err := d.guard(); err != nil {
		return err
	}
	if !d.options.Suspend {
		return errors.New("suspend not enabled: hardware validation required")
	}
	if until.Before(time.Now().Add(5 * time.Second)) {
		return errors.New("wake alarm must be at least five seconds in future")
	}
	if d.read("/sys/class/power_supply/bd71827_bat/status") != "Discharging" {
		return errors.New("refusing MediaTek suspend: battery must report Discharging (USB power may hang the kernel)")
	}
	if !strings.Contains(d.read("/sys/power/state"), "mem") {
		return errors.New("kernel does not advertise mem suspend")
	}
	for _, p := range []string{"/dev/rtc0", "/sys/power/state-extended"} {
		if _, err := os.Stat(d.path(p)); err != nil {
			return fmt.Errorf("suspend prerequisite %s: %w", p, err)
		}
	}
	if err := d.SetWiFi(ctx, false); err != nil {
		return err
	}
	if err := d.rtcAlarm(until, true); err != nil {
		return err
	}
	defer d.rtcAlarm(until, false)
	if err := d.write("/sys/power/state-extended", "1"); err != nil {
		return err
	}
	defer d.write("/sys/power/state-extended", "0")
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	// Recheck immediately before entering suspend: USB may have been inserted.
	if d.read("/sys/class/power_supply/bd71827_bat/status") != "Discharging" {
		return errors.New("suspend cancelled: power state changed")
	}
	if err := exec.CommandContext(ctx, "sync").Run(); err != nil {
		return err
	}
	return d.write("/sys/power/state", "mem")
}
