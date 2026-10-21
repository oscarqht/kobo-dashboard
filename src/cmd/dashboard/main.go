package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"kobo-dashboard/src/internal/app"
	"kobo-dashboard/src/internal/device"
	"kobo-dashboard/src/internal/providers"
	"kobo-dashboard/src/internal/render"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	data := flag.String("data", "./data", "private configuration and cache directory")
	listen := flag.String("listen", "127.0.0.1:8787", "desktop preview address (device editor uses port only)")
	demo := flag.Bool("demo", false, "explicit sample data for desktop preview")
	native := flag.Bool("device", false, "enable Kobo hardware access")
	suspend := flag.Bool("suspend", false, "enable guarded RTC suspend after device validation")
	doctor := flag.Bool("doctor", false, "print read-only device diagnostics and exit")
	fbink := flag.String("fbink", filepath.Join(base, "fbink"), "FBInk executable with PNG support")
	touch := flag.String("touch-device", "", "touch event path (autodetect when supported)")
	power := flag.String("power-device", "", "power key event path")
	wifiOn := flag.String("wifi-on", filepath.Join(filepath.Dir(base), "config", "wifi-on.sh"), "Wi-Fi connection helper")
	wifiOff := flag.String("wifi-off", filepath.Join(filepath.Dir(base), "config", "wifi-off.sh"), "Wi-Fi disconnect helper")
	mirror := flag.Bool("mirror-y", true, "mirror Elipsa 2E touch Y axis")
	maxX := flag.Int("touch-max-x", 0, "raw touch X maximum; zero uses device detection")
	maxY := flag.Int("touch-max-y", 0, "raw touch Y maximum; zero uses device detection")
	credentials := flag.String("credentials", "", "import USB authorization file into private data directory")
	flag.Parse()
	if *native && *demo {
		return fmt.Errorf("demo mode is desktop only; omit --device")
	}
	dev := device.New(device.Options{Enabled: *native, Suspend: *suspend, FBInkPath: *fbink, TouchPath: *touch, PowerPath: *power, WiFiOn: *wifiOn, WiFiOff: *wifiOff, MirrorY: *mirror, TouchMaxX: *maxX, TouchMaxY: *maxY})
	if *doctor {
		b, err := json.MarshalIndent(dev.Doctor(), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		return err
	}
	if *credentials != "" {
		c, err := providers.ReadCredentials(*credentials)
		if err != nil {
			return fmt.Errorf("import authorization: %w", err)
		}
		target := filepath.Join(*data, "credentials.json")
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("authorization already exists; remove private credentials.json before importing replacement")
		}
		if err := providers.WriteCredentials(target, c); err != nil {
			return err
		}
		log.Print("Authorization imported. Remove the transfer file from USB storage.")
	}
	e, err := app.New(*data, *demo)
	if err != nil {
		return err
	}
	if c, err := providers.ReadCredentials(filepath.Join(*data, "credentials.json")); err == nil {
		e.SetCredentials(c)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read authorization: %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *native {
		return runDevice(ctx, e, dev, *listen, *data, *suspend)
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("desktop preview must bind loopback; device editor is explicitly opened on Kobo")
	}
	s, err := app.NewServer(e, *demo)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		server.Shutdown(shutdown)
	}()
	go func() {
		select {
		case <-e.Exit:
			cancel()
		case <-ctx.Done():
		}
	}()
	go func() {
		refreshCtx, c := context.WithTimeout(ctx, 2*time.Minute)
		defer c()
		if err := e.Refresh(refreshCtx); err != nil {
			log.Print(err)
		}
	}()
	go func() {
		for {
			timer := time.NewTimer(time.Until(nextHour()))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				refreshCtx, c := context.WithTimeout(ctx, 2*time.Minute)
				_ = e.Refresh(refreshCtx)
				c()
			}
		}
	}()
	log.Printf("Desktop preview: http://%s (sample data: %t)", *listen, *demo)
	if !*demo {
		log.Printf("Pairing code: %s", s.Code)
	}
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func nextHour() time.Time { return nextHourAt(time.Now()) }
func draw(e *app.Engine, d interface{ ShowPNG(string) error }, dir string) error {
	c, s, v := e.State()
	b, err := render.Render(c, s, v)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "snapshot.png")
	if err = os.WriteFile(path, b, 0600); err != nil {
		return err
	}
	return d.ShowPNG(path)
}

func nextHourAt(now time.Time) time.Time { return now.Truncate(time.Hour).Add(time.Hour) }
