package status

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/tetsuh/tt-env-go/pkg/kmd"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// Summary is the aggregated environment status rendered by `tt-env status`.
type Summary struct {
	// Hardware lists detected Tenstorrent PCI devices.
	Hardware []Device
	// HardwareErr is set when hardware detection failed; rendering degrades
	// gracefully rather than aborting.
	HardwareErr error
	// ActiveRelease is the active release name, or "" when none is active.
	ActiveRelease string
	// InstalledReleases lists installed release names.
	InstalledReleases []string
	// KMD is the Tenstorrent kernel module version state.
	KMD kmd.ModuleVersion
	// SecureBoot is the UEFI Secure Boot state.
	SecureBoot SecureBootResult
}

// SecureBootResult aliases the kmd Secure Boot result so callers of pkg/status
// do not need to import pkg/kmd directly for the common case.
type SecureBootResult = kmd.SecureBootResult

// SecureBootChecker aliases the kmd Secure Boot checker.
type SecureBootChecker = kmd.SecureBootChecker

// Reporter aggregates the individual probes into a Summary. Each probe is
// injectable so the aggregation can be tested without touching the host.
type Reporter struct {
	// Detector probes for Tenstorrent hardware. Required.
	Detector *Detector
	// Installer probes the active and installed releases. Required.
	Installer *version.Installer
	// SecureBoot probes the UEFI Secure Boot state. Required.
	SecureBoot *kmd.SecureBootChecker
	// KMDVersion probes the kernel module version. Required.
	KMDVersion *kmd.VersionProber
}

// Report gathers all probes into a Summary. Probe failures are captured in the
// Summary (for example HardwareErr, or an empty ActiveRelease) rather than
// returned, so the command can always present a best-effort status.
func (r *Reporter) Report(ctx context.Context) Summary {
	var s Summary

	s.Hardware, s.HardwareErr = r.Detector.Detect(ctx)

	if active, err := r.Installer.Current(); err == nil {
		s.ActiveRelease = active
	}
	if installed, err := r.Installer.List(); err == nil {
		s.InstalledReleases = installed
	}

	s.KMD = r.KMDVersion.Probe(ctx)
	s.SecureBoot = r.SecureBoot.Check(ctx)

	return s
}

// Render writes a concise, human-readable status report to w.
func (s Summary) Render(w io.Writer) error {
	var b strings.Builder

	b.WriteString("Tenstorrent environment status\n")

	b.WriteString("  Hardware:           ")
	switch {
	case s.HardwareErr != nil:
		fmt.Fprintf(&b, "detection unavailable (%v)\n", s.HardwareErr)
	case len(s.Hardware) == 0:
		b.WriteString("no Tenstorrent devices detected\n")
	default:
		fmt.Fprintf(&b, "%d device(s)\n", len(s.Hardware))
		for _, d := range s.Hardware {
			fmt.Fprintf(&b, "    %s  %s [%s:%s]\n", d.Address, deviceLabel(d), d.VendorID, d.DeviceID)
		}
	}

	fmt.Fprintf(&b, "  Active release:     %s\n", orNone(s.ActiveRelease))
	fmt.Fprintf(&b, "  Installed releases: %s\n", orNone(strings.Join(s.InstalledReleases, ", ")))
	fmt.Fprintf(&b, "  KMD module:         %s\n", kmdVersionLabel(s.KMD))
	fmt.Fprintf(&b, "  Secure Boot:        %s\n", string(s.SecureBoot.State))

	_, err := io.WriteString(w, b.String())
	return err
}

func deviceLabel(d Device) string {
	if d.Description != "" {
		return d.Description
	}
	return "Tenstorrent device"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func kmdVersionLabel(v kmd.ModuleVersion) string {
	switch {
	case !v.Loaded:
		return "(not loaded)"
	case v.Version == "":
		return "(unknown)"
	default:
		return v.Version
	}
}
