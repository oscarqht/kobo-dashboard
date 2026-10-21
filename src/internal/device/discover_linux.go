//go:build linux

package device

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func inputBits(fd uintptr, event uint32) [128]byte {
	var b [128]byte
	syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(0x80804520+event), uintptr(unsafe.Pointer(&b[0])))
	return b
}
func hasBit(b [128]byte, n int) bool { return b[n/8]&(1<<uint(n%8)) != 0 }
func absMax(fd uintptr, axis int) int {
	var a [6]int32
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(0x80184540)+uintptr(axis), uintptr(unsafe.Pointer(&a[0])))
	if err != 0 || a[1] != 0 {
		return 0
	}
	return int(a[2])
}
func (d *Device) discoverInputs() {
	paths, _ := filepath.Glob(d.path("/dev/input/event*"))
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		abs := inputBits(f.Fd(), 3)
		keys := inputBits(f.Fd(), 1)
		if d.options.PowerPath == "" && hasBit(keys, 116) {
			d.options.PowerPath = p
		}
		if d.options.TouchPath == "" {
			x, y := 0, 1
			if hasBit(abs, 53) && hasBit(abs, 54) {
				x, y = 53, 54
			}
			if hasBit(abs, x) && hasBit(abs, y) && hasBit(keys, 330) {
				maxX, maxY := absMax(f.Fd(), x), absMax(f.Fd(), y)
				if maxX > 0 && maxY > 0 {
					d.options.TouchPath = p
					if d.options.TouchMaxX == 0 {
						d.options.TouchMaxX = maxX
					}
					if d.options.TouchMaxY == 0 {
						d.options.TouchMaxY = maxY
					}
				}
			}
		}
		f.Close()
	}
}
