package device

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
)

// ParseInput reads a Linux input_event; ARMv7 uses 32-bit timeval (16 bytes),
// while 64-bit development systems use 24 bytes. Explicit size prevents ABI drift.
func ParseInput(r io.Reader, size int) (uint16, uint16, int32, error) {
	if size != 16 && size != 24 {
		return 0, 0, 0, fmt.Errorf("invalid input event size %d", size)
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, 0, 0, err
	}
	at := size - 8
	return binary.LittleEndian.Uint16(b[at:]), binary.LittleEndian.Uint16(b[at+2:]), int32(binary.LittleEndian.Uint32(b[at+4:])), nil
}
func (d *Device) Events(ctx context.Context) <-chan Event {
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		if err := d.guard(); err != nil {
			out <- Event{Error: err.Error()}
			return
		}
		if d.options.TouchPath == "" {
			out <- Event{Error: "touch path missing: use doctor to identify and calibrate input"}
			return
		}
		touch, err := os.Open(d.options.TouchPath)
		if err != nil {
			out <- Event{Error: err.Error()}
			return
		}
		defer touch.Close()
		child, cancel := context.WithCancel(ctx)
		var workers sync.WaitGroup
		defer func() { cancel(); workers.Wait() }()
		go func() { <-child.Done(); touch.Close() }()
		send := func(e Event) {
			select {
			case out <- e:
			case <-child.Done():
			}
		}
		if d.options.PowerPath != "" && d.options.PowerPath != d.options.TouchPath {
			workers.Add(1)
			go func() { defer workers.Done(); d.powerEvents(child, send) }()
		}
		size := 16
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			size = 24
		}
		x, y := 0, 0
		slot := int32(0)
		down, release := false, false
		for {
			typ, code, value, err := ParseInput(touch, size)
			if err != nil {
				if ctx.Err() == nil {
					send(Event{Error: err.Error()})
				}
				return
			}
			switch typ {
			case 1: // EV_KEY
				if code == 116 && value == 0 {
					send(Event{Action: "power"})
				}
				if code == 330 {
					if value > 0 {
						down = true
					} else if down {
						release = true
					}
				}
			case 3: // EV_ABS, slot zero only
				if code == 47 {
					slot = value
				}
				if slot != 0 {
					continue
				}
				switch code {
				case 0, 53:
					x = int(value)
				case 1, 54:
					y = int(value)
				case 57:
					if value >= 0 {
						down = true
					} else if down {
						release = true
					}
				case 58:
					if value > 0 {
						down = true
					} else if down {
						release = true
					}
				}
			case 0: // EV_SYN
				if code == 0 && release {
					maxX, maxY := d.options.TouchMaxX, d.options.TouchMaxY
					if maxX <= 0 {
						maxX = d.options.Width - 1
					}
					if maxY <= 0 {
						maxY = d.options.Height - 1
					}
					px, py := x*(d.options.Width-1)/maxX, y*(d.options.Height-1)/maxY
					if d.options.MirrorY {
						py = d.options.Height - 1 - py
					}
					if px < 0 {
						px = 0
					}
					if py < 0 {
						py = 0
					}
					if px >= d.options.Width {
						px = d.options.Width - 1
					}
					if py >= d.options.Height {
						py = d.options.Height - 1
					}
					send(Event{Action: "tap", X: px, Y: py})
					down = false
					release = false
				}
			}
		}
	}()
	return out
}
func (d *Device) powerEvents(ctx context.Context, send func(Event)) {
	f, err := os.Open(d.options.PowerPath)
	if err != nil {
		send(Event{Error: err.Error()})
		return
	}
	defer f.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			f.Close()
		case <-done:
		}
	}()
	size := 16
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		size = 24
	}
	for {
		typ, code, value, err := ParseInput(f, size)
		if err != nil {
			return
		}
		if typ == 1 && code == 116 && value == 0 {
			send(Event{Action: "power"})
		}
	}
}
