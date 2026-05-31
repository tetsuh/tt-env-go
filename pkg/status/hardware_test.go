package status

import (
	"context"
	"errors"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

const lspciWithTenstorrent = `0000:00:00.0 Host bridge [0600]: Intel Corporation Device [8086:1234] (rev 05)
0000:00:1f.0 ISA bridge [0601]: Intel Corporation Device [8086:a306]
0000:01:00.0 Processing accelerators [1200]: Tenstorrent Inc. Wormhole [1e52:401e] (rev 01)
0000:02:00.0 Processing accelerators [1200]: Tenstorrent Inc. Grayskull [1e52:faca]
`

const lspciNoTenstorrent = `0000:00:00.0 Host bridge [0600]: Intel Corporation Device [8086:1234] (rev 05)
0000:00:02.0 VGA compatible controller [0300]: Intel Corporation Device [8086:3e98]
`

func TestDetectFindsTenstorrentDevices(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte(lspciWithTenstorrent)},
	}}
	d := &Detector{Runner: runner}

	devices, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2: %+v", len(devices), devices)
	}

	want := Device{
		Address:     "0000:01:00.0",
		Class:       "Processing accelerators",
		Description: "Tenstorrent Inc. Wormhole",
		VendorID:    "1e52",
		DeviceID:    "401e",
		Raw:         "0000:01:00.0 Processing accelerators [1200]: Tenstorrent Inc. Wormhole [1e52:401e] (rev 01)",
	}
	if devices[0] != want {
		t.Errorf("device[0] = %+v\nwant %+v", devices[0], want)
	}
	if devices[1].DeviceID != "faca" || devices[1].Address != "0000:02:00.0" {
		t.Errorf("device[1] = %+v, want addr 0000:02:00.0 / device faca", devices[1])
	}

	if got := runner.CommandStrings(); len(got) != 1 || got[0] != "lspci -Dnn" {
		t.Errorf("commands = %v, want [lspci -Dnn]", got)
	}
}

func TestDetectNoHardwareReturnsEmpty(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte(lspciNoTenstorrent)},
	}}
	d := &Detector{Runner: runner}

	devices, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("got %d devices, want 0: %+v", len(devices), devices)
	}
}

func TestDetectEmptyOutput(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("")},
	}}
	d := &Detector{Runner: runner}

	devices, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if devices != nil {
		t.Errorf("devices = %+v, want nil", devices)
	}
}

func TestDetectCommandError(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("lspci: command not found"), Err: errors.New("exit status 127")},
	}}
	d := &Detector{Runner: runner}

	if _, err := d.Detect(context.Background()); err == nil {
		t.Fatal("Detect() = nil error, want command error")
	}
}

func TestDetectCaseInsensitiveVendor(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("0000:01:00.0 Processing accelerators [1200]: Tenstorrent Inc. Device [1E52:401E] (rev 01)\n")},
	}}
	d := &Detector{Runner: runner}

	devices, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(devices))
	}
	if devices[0].VendorID != "1e52" || devices[0].DeviceID != "401e" {
		t.Errorf("ids = %s:%s, want 1e52:401e (lowercased)", devices[0].VendorID, devices[0].DeviceID)
	}
}

func TestDetectCustomVendorID(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("0000:03:00.0 Processing accelerators [1200]: Acme Widget [abcd:0001]\n" + lspciWithTenstorrent)},
	}}
	d := &Detector{Runner: runner, VendorID: "abcd"}

	devices, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(devices) != 1 || devices[0].VendorID != "abcd" {
		t.Fatalf("got %+v, want single abcd device", devices)
	}
}

func TestDetectCustomLspciPath(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte(lspciNoTenstorrent)},
	}}
	d := &Detector{Runner: runner, Lspci: "/usr/bin/lspci"}

	if _, err := d.Detect(context.Background()); err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got := runner.CommandStrings(); len(got) != 1 || got[0] != "/usr/bin/lspci -Dnn" {
		t.Errorf("commands = %v, want [/usr/bin/lspci -Dnn]", got)
	}
}

func TestParseDeviceMissingClassColon(t *testing.T) {
	// Defensive: a line without the "]: " class separator should still yield
	// address and vendor/device ids.
	dev := parseDevice("0000:04:00.0 Tenstorrent Inc. Device [1e52:401e]")
	if dev.Address != "0000:04:00.0" {
		t.Errorf("Address = %q", dev.Address)
	}
	if dev.VendorID != "1e52" || dev.DeviceID != "401e" {
		t.Errorf("ids = %s:%s, want 1e52:401e", dev.VendorID, dev.DeviceID)
	}
}
