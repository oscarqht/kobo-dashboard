package main

import (
	"context"
	"fmt"
	"kobo-dashboard/src/internal/app"
	"kobo-dashboard/src/internal/device"
	"kobo-dashboard/src/internal/render"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

type nativeDevice interface {
	Doctor() device.Report
	Events(context.Context) <-chan device.Event
	ShowPNG(string) error
	SetWiFi(context.Context, bool) error
	SuspendUntil(context.Context, time.Time) error
}

func runDevice(ctx context.Context, e *app.Engine, d nativeDevice, listen, dir string, suspend bool) error {
	report := d.Doctor()
	if report.Runtime != "linux/arm" {
		return fmt.Errorf("native mode requires Kobo Linux/ARM; run --doctor for diagnostics")
	}
	if report.Framebuffer == "" {
		return fmt.Errorf("framebuffer not found; run --doctor")
	}
	if report.TouchPath == "" {
		return fmt.Errorf("touch input not detected; run --doctor and calibrate before launching")
	}
	events := d.Events(ctx)
	refresh := make(chan struct{}, 1)
	activity := make(chan struct{}, 1)
	request := func() {
		select {
		case refresh <- struct{}{}:
		default:
		}
	}
	var editor *app.Server
	var httpServer *http.Server
	wifiOff := func() {
		clean, c := context.WithTimeout(context.Background(), 50*time.Second)
		defer c()
		if err := d.SetWiFi(clean, false); err != nil {
			log.Print(err)
		}
	}
	stopEditor := func() {
		if httpServer != nil {
			shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
			_ = httpServer.Shutdown(shutdown)
			c()
			httpServer = nil
			editor = nil
			e.SetEditor("", "")
			wifiOff()
		}
	}
	defer stopEditor()
	openEditor := func() error {
		if editor != nil {
			return nil
		}
		netCtx, c := context.WithTimeout(ctx, 50*time.Second)
		defer c()
		if err := d.SetWiFi(netCtx, true); err != nil {
			return err
		}
		s, err := app.NewServer(e, false)
		if err != nil {
			return err
		}
		s.Fetch = request
		_, port, err := net.SplitHostPort(listen)
		if err != nil {
			return err
		}
		if _, err := strconv.Atoi(port); err != nil {
			return err
		}
		ln, err := net.Listen("tcp", ":"+port)
		if err != nil {
			return err
		}
		s.OnAction = func(string) {
			select {
			case activity <- struct{}{}:
			default:
			}
		}
		editor = s
		httpServer = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
		go func(server *http.Server) {
			if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Print(err)
			}
		}(httpServer)
		address := ""
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() {
				address = "http://" + net.JoinHostPort(ip.String(), port)
				break
			}
		}
		if address == "" {
			stopEditor()
			return fmt.Errorf("no Wi-Fi IPv4 address available")
		}
		e.SetEditor(s.Code, address)
		return nil
	}
	doRefresh := func() {
		defer func() {
			if editor == nil {
				wifiOff()
			}
		}()
		fetch, c := context.WithTimeout(ctx, 2*time.Minute)
		defer c()
		if err := d.SetWiFi(fetch, true); err != nil {
			e.MarkNetworkError()
			log.Print(err)
			return
		}
		if err := e.Refresh(fetch); err != nil {
			log.Print(err)
		}
	}
	if err := draw(e, d, dir); err != nil {
		return err
	}
	doRefresh()
	if err := draw(e, d, dir); err != nil {
		return err
	}
	due := nextHour()
	awakeUntil := time.Now().Add(time.Minute)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-e.Exit:
			return nil
		case <-activity:
			awakeUntil = time.Now().Add(time.Minute)
		case <-refresh:
			doRefresh()
			awakeUntil = time.Now().Add(time.Minute)
		case <-e.Changed:
			if err := draw(e, d, dir); err != nil {
				return err
			}
		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("input unavailable; exiting to Kobo reader; see --doctor")
			}
			if ev.Error != "" {
				return fmt.Errorf("input unavailable: %s", ev.Error)
			}
			awakeUntil = time.Now().Add(time.Minute)
			if ev.Action == "power" {
				continue
			}
			if ev.Action == "tap" {
				c, s, v := e.State()
				hits := render.Hits(c, s, v)
				for i := len(hits) - 1; i >= 0; i-- {
					h := hits[i]
					if ev.X < h.X || ev.X >= h.X+h.W || ev.Y < h.Y || ev.Y >= h.Y+h.H {
						continue
					}
					switch h.Action {
					case "refresh":
						request()
					case "close":
						if editor != nil {
							stopEditor()
						} else {
							_ = e.Action("close", h.WidgetID, h.Value)
						}
					case "editor":
						if editor != nil {
							stopEditor()
						} else if err := openEditor(); err != nil {
							log.Print(err)
						}
					default:
						if err := e.Action(h.Action, h.WidgetID, h.Value); err != nil {
							log.Print(err)
						}
						if h.Action == "next" || h.Action == "previous" || h.Action == "today" || h.Action == "mode" {
							request()
						}
					}
					break
				}
			}
		case <-tick.C:
			if editor != nil && editor.Expired() {
				stopEditor()
				awakeUntil = time.Now().Add(time.Minute)
			}
			if !time.Now().Before(due) {
				doRefresh()
				due = nextHour()
				if err := draw(e, d, dir); err != nil {
					return err
				}
			}
			if editor == nil && !time.Now().Before(awakeUntil) {
				e.ResetView()
				if err := draw(e, d, dir); err != nil {
					return err
				}
				if suspend {
					err := d.SuspendUntil(ctx, due)
					if err != nil {
						log.Printf("Sleep unavailable: %v", err)
						awakeUntil = time.Now().Add(time.Minute)
					} else {
						// A hardware alarm or power button resumes this call. The next tick fetches if due.
						awakeUntil = time.Now().Add(time.Minute)
					}
				} else {
					awakeUntil = time.Now().Add(time.Minute)
				}
			}
		}
	}
}
