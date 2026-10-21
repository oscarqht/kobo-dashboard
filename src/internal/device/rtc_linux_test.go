//go:build linux

package device

import (
	"testing"
	"unsafe"
)

func TestRTCABI(t *testing.T) {
	var a rtcAlarm
	if unsafe.Sizeof(a) != 40 || unsafe.Offsetof(a.Time) != 4 {
		t.Fatalf("RTC ABI size=%d time offset=%d", unsafe.Sizeof(a), unsafe.Offsetof(a.Time))
	}
}
