//go:build linux

package device

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// Linux UAPI struct rtc_wkalrm has two bytes, two padding bytes, nine int32s.
type rtcAlarm struct {
	Enabled, Pending uint8
	Padding          [2]byte
	Time             [9]int32
}

func (d *Device) rtcAlarm(until time.Time, enabled bool) error {
	f, err := os.OpenFile(d.path("/dev/rtc0"), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	t := until.UTC()
	a := rtcAlarm{Time: [9]int32{int32(t.Second()), int32(t.Minute()), int32(t.Hour()), int32(t.Day()), int32(t.Month()) - 1, int32(t.Year()) - 1900, -1, -1, -1}}
	if enabled {
		a.Enabled = 1
	}
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4028700f, uintptr(unsafe.Pointer(&a)))
	if e != 0 {
		return fmt.Errorf("RTC_WKALM_SET: %w", e)
	}
	var read rtcAlarm
	_, _, e = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x80287010, uintptr(unsafe.Pointer(&read)))
	if e != 0 {
		if enabled {
			a.Enabled = 0
			syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4028700f, uintptr(unsafe.Pointer(&a)))
		}
		return fmt.Errorf("RTC_WKALM_RD: %w", e)
	}
	matches := read.Enabled == a.Enabled
	if enabled {
		for i := 0; i < 6; i++ {
			if read.Time[i] != a.Time[i] {
				matches = false
			}
		}
	}
	if !matches {
		a.Enabled = 0
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4028700f, uintptr(unsafe.Pointer(&a)))
		return fmt.Errorf("RTC did not preserve the requested wake alarm")
	}
	return nil
}
