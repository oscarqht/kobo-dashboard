package device

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestInputABI(t *testing.T) {
	for _, size := range []int{16, 24} {
		b := make([]byte, size)
		binary.LittleEndian.PutUint16(b[size-8:], 3)
		binary.LittleEndian.PutUint16(b[size-6:], 57)
		binary.LittleEndian.PutUint32(b[size-4:], 0xffffffff)
		typ, code, value, err := ParseInput(bytes.NewReader(b), size)
		if err != nil || typ != 3 || code != 57 || value != -1 {
			t.Fatalf("%d: %d %d %d %v", size, typ, code, value, err)
		}
	}
}
func TestDoctorReadOnlyFixture(t *testing.T) {
	root := t.TempDir()
	for p, v := range map[string]string{"sys/class/power_supply/bd71827_bat/status": "Discharging\n", "sys/class/power_supply/bd71827_bat/capacity": "82\n", "mnt/onboard/.kobo/version": "fixture firmware"} {
		path := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte(v), 0600)
	}
	r := New(Options{Root: root}).Doctor()
	if r.Capacity != "82" || r.Status != "Discharging" || r.Firmware != "fixture firmware" {
		t.Fatalf("%+v", r)
	}
	if r.Enabled || len(r.Missing) == 0 {
		t.Fatalf("%+v", r)
	}
}
func TestDisabledHardware(t *testing.T) {
	d := New(Options{})
	if d.ShowPNG("absent.png") == nil {
		t.Fatal("preview must not write hardware")
	}
}
