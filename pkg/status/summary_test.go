package status

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/kmd"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

func TestSummaryRenderPopulated(t *testing.T) {
	s := Summary{
		Hardware: []Device{
			{Address: "0000:01:00.0", Description: "Tenstorrent Wormhole", VendorID: "1e52", DeviceID: "401e"},
		},
		ActiveRelease:     "2026.05.16",
		InstalledReleases: []string{"2026.04.01", "2026.05.16"},
		KMD:               kmd.ModuleVersion{Loaded: true, Version: "1.2.3"},
		SecureBoot:        kmd.SecureBootResult{State: kmd.SecureBootDisabled},
	}
	var b strings.Builder
	if err := s.Render(&b); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"1 device(s)", "0000:01:00.0", "Tenstorrent Wormhole", "[1e52:401e]",
		"2026.05.16", "2026.04.01, 2026.05.16", "1.2.3", "disabled",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render output missing %q\n%s", want, out)
		}
	}
}

func TestSummaryRenderDegraded(t *testing.T) {
	s := Summary{
		HardwareErr: errors.New("lspci missing"),
		KMD:         kmd.ModuleVersion{Loaded: false},
		SecureBoot:  kmd.SecureBootResult{State: kmd.SecureBootUnknown},
	}
	var b strings.Builder
	if err := s.Render(&b); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"detection unavailable", "lspci missing",
		"Active release:     (none)", "Installed releases: (none)",
		"(not loaded)", "unknown",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render output missing %q\n%s", want, out)
		}
	}
}

func TestSummaryRenderKMDUnknownVersion(t *testing.T) {
	s := Summary{KMD: kmd.ModuleVersion{Loaded: true, Version: ""}}
	var b strings.Builder
	if err := s.Render(&b); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(b.String(), "(unknown)") {
		t.Errorf("Render output should contain (unknown) for loaded module with no version\n%s", b.String())
	}
}

func TestReporterReport(t *testing.T) {
	// Installer with one active + installed release.
	root := t.TempDir()
	inst := &version.Installer{Root: root}
	if _, err := inst.Install("2026.05.16", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := inst.Use("2026.05.16"); err != nil {
		t.Fatal(err)
	}

	// Loaded KMD with version via mock modinfo + temp sysfs.
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "tenstorrent"), 0o755); err != nil {
		t.Fatal(err)
	}

	hwRunner := &packagemanager.MockRunner{
		Responses: []packagemanager.CommandResponse{
			{Output: []byte("0000:01:00.0 Class [1234]: Tenstorrent Inc. Wormhole [1e52:401e]\n")},
		},
	}
	kmdRunner := &packagemanager.MockRunner{
		Responses: []packagemanager.CommandResponse{{Output: []byte("1.2.3\n")}},
	}

	r := &Reporter{
		Detector:   &Detector{Runner: hwRunner},
		Installer:  inst,
		SecureBoot: &kmd.SecureBootChecker{EFIDir: filepath.Join(t.TempDir(), "nonexistent")},
		KMDVersion: &kmd.VersionProber{Runner: kmdRunner, SysModuleDir: sys},
	}

	got := r.Report(context.Background())
	if got.ActiveRelease != "2026.05.16" {
		t.Errorf("ActiveRelease = %q, want %q", got.ActiveRelease, "2026.05.16")
	}
	if len(got.InstalledReleases) != 1 || got.InstalledReleases[0] != "2026.05.16" {
		t.Errorf("InstalledReleases = %v, want [2026.05.16]", got.InstalledReleases)
	}
	if len(got.Hardware) != 1 {
		t.Fatalf("Hardware = %v, want 1 device", got.Hardware)
	}
	if got.HardwareErr != nil {
		t.Errorf("HardwareErr = %v, want nil", got.HardwareErr)
	}
	if !got.KMD.Loaded || got.KMD.Version != "1.2.3" {
		t.Errorf("KMD = %+v, want loaded 1.2.3", got.KMD)
	}
	if !got.SecureBoot.Safe() {
		t.Errorf("SecureBoot = %+v, want Safe", got.SecureBoot)
	}
}
