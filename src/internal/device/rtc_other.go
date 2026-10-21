//go:build !linux

package device

import (
	"errors"
	"time"
)

func (d *Device) rtcAlarm(time.Time, bool) error { return errors.New("RTC ioctl requires Linux") }
