// Package status probes the host for Tenstorrent hardware and environment
// state. The hardware detector enumerates PCI devices via lspci and filters for
// the Tenstorrent PCI vendor id.
package status

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// DefaultVendorID is the Tenstorrent PCI vendor id.
const DefaultVendorID = "1e52"

// DefaultLspci is the executable used to enumerate PCI devices.
const DefaultLspci = "lspci"

// envVendorID overrides the detected vendor id, mirroring proto1 lib/status.sh.
const envVendorID = "TT_STATUS_TT_VENDOR_ID"

// vendorDevicePattern matches the "[vendor:device]" numeric id pair emitted by
// `lspci -nn` (for example "[1e52:401e]").
var vendorDevicePattern = regexp.MustCompile(`\[([0-9a-fA-F]{4}):([0-9a-fA-F]{4})\]`)

// Device is a detected PCI device.
type Device struct {
	// Address is the PCI slot address, for example "0000:01:00.0".
	Address string
	// Class is the human-readable PCI class, for example "Processing
	// accelerators".
	Class string
	// Description is the device description, for example "Tenstorrent Inc.
	// Wormhole".
	Description string
	// VendorID is the numeric PCI vendor id, for example "1e52".
	VendorID string
	// DeviceID is the numeric PCI device id, for example "401e".
	DeviceID string
	// Raw is the full lspci line the device was parsed from.
	Raw string
}

// Detector enumerates PCI devices and filters for Tenstorrent hardware.
type Detector struct {
	// Runner executes lspci. If nil, ExecRunner is used.
	Runner packagemanager.CommandRunner
	// Lspci is the executable to invoke. If empty, DefaultLspci is used.
	Lspci string
	// VendorID is the PCI vendor id to match. If empty, the
	// TT_STATUS_TT_VENDOR_ID environment variable then DefaultVendorID are
	// used.
	VendorID string
}

func (d *Detector) runner() packagemanager.CommandRunner {
	if d.Runner != nil {
		return d.Runner
	}
	return packagemanager.ExecRunner{}
}

func (d *Detector) lspci() string {
	if d.Lspci != "" {
		return d.Lspci
	}
	return DefaultLspci
}

func (d *Detector) vendorID() string {
	if v := strings.ToLower(strings.TrimSpace(d.VendorID)); v != "" {
		return v
	}
	if env := strings.ToLower(strings.TrimSpace(os.Getenv(envVendorID))); env != "" {
		return env
	}
	return DefaultVendorID
}

// Detect runs `lspci -Dnn` and returns the Tenstorrent devices it finds. It
// returns an empty, non-error result when no matching hardware is present.
func (d *Detector) Detect(ctx context.Context) ([]Device, error) {
	out, err := d.runner().Run(ctx, d.lspci(), "-Dnn")
	if err != nil {
		return nil, commandError(d.lspci()+" -Dnn", out, err)
	}

	vendor := d.vendorID()
	marker := "[" + vendor + ":"

	var devices []Device
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(line), marker) {
			continue
		}
		devices = append(devices, parseDevice(line))
	}
	return devices, nil
}

// parseDevice extracts the structured fields from a single `lspci -Dnn` line.
// Unparseable fields are left empty; Raw always holds the original line.
func parseDevice(line string) Device {
	dev := Device{Raw: line}

	addr, rest, found := strings.Cut(line, " ")
	dev.Address = addr
	if !found {
		return dev
	}

	// rest looks like: "<class> [<classid>]: <description> [<vendor>:<device>] (rev nn)"
	classPart, descPart, hasColon := strings.Cut(rest, "]: ")
	if hasColon {
		dev.Class = trimBracketSuffix(classPart)
		dev.Description = trimBracketSuffix(descPart)
	} else {
		dev.Description = trimBracketSuffix(rest)
	}

	if m := vendorDevicePattern.FindStringSubmatch(line); m != nil {
		dev.VendorID = strings.ToLower(m[1])
		dev.DeviceID = strings.ToLower(m[2])
	}
	return dev
}

// trimBracketSuffix drops a trailing " [....]" id tag and any "(rev nn)" suffix,
// returning the leading human-readable text.
func trimBracketSuffix(s string) string {
	if i := strings.Index(s, " ["); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// commandError wraps a failed command invocation with its combined output.
func commandError(cmd string, out []byte, err error) error {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return fmt.Errorf("status: command %q failed: %w", cmd, err)
	}
	return fmt.Errorf("status: command %q failed: %w: %s", cmd, err, trimmed)
}
