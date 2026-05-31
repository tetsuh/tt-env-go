package version

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newInstaller(t *testing.T) *Installer {
	t.Helper()
	return &Installer{
		Root: t.TempDir(),
		now:  func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to not exist, stat err = %v", path, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func TestInstallFresh(t *testing.T) {
	i := newInstaller(t)

	var stagedDir string
	res, err := i.Install("v1.0.0", func(dir string) error {
		stagedDir = dir
		return os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("data"), 0o644)
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if !res.Installed {
		t.Errorf("Installed = false, want true")
	}
	if res.Release != "v1.0.0" {
		t.Errorf("Release = %q, want v1.0.0", res.Release)
	}
	if res.Path != i.ReleaseDir("v1.0.0") {
		t.Errorf("Path = %q, want %q", res.Path, i.ReleaseDir("v1.0.0"))
	}

	// Final directory, staged content, and marker exist; no partial remains.
	mustExist(t, i.ReleaseDir("v1.0.0"))
	mustExist(t, filepath.Join(i.ReleaseDir("v1.0.0"), "artifact.txt"))
	mustExist(t, filepath.Join(i.ReleaseDir("v1.0.0"), markerName))
	mustNotExist(t, i.partialDir("v1.0.0"))
	mustNotExist(t, stagedDir) // staging dir was renamed away

	if !i.IsInstalled("v1.0.0") {
		t.Errorf("IsInstalled = false, want true")
	}

	m, err := i.Marker("v1.0.0")
	if err != nil {
		t.Fatalf("Marker: %v", err)
	}
	if m.Release != "v1.0.0" {
		t.Errorf("marker.Release = %q, want v1.0.0", m.Release)
	}
	if m.SchemaVersion != markerSchemaVersion {
		t.Errorf("marker.SchemaVersion = %d, want %d", m.SchemaVersion, markerSchemaVersion)
	}
	if !m.InstalledAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("marker.InstalledAt = %v, unexpected", m.InstalledAt)
	}
}

func TestInstallInterruptedLeavesPartialNotPromoted(t *testing.T) {
	i := newInstaller(t)

	sentinel := errors.New("boom")
	res, err := i.Install("v1.0.0", func(dir string) error {
		if werr := os.WriteFile(filepath.Join(dir, "half.txt"), []byte("x"), 0o644); werr != nil {
			return werr
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("Install err = %v, want wrap of sentinel", err)
	}
	if res.Installed {
		t.Errorf("Installed = true, want false on failure")
	}

	// Previous state untouched: no promoted release directory.
	mustNotExist(t, i.ReleaseDir("v1.0.0"))
	// Partial remains for inspection, with its staged content.
	mustExist(t, i.partialDir("v1.0.0"))
	mustExist(t, filepath.Join(i.partialDir("v1.0.0"), "half.txt"))
	// No marker in the (un-promoted) partial because staging failed first.
	mustNotExist(t, filepath.Join(i.partialDir("v1.0.0"), markerName))

	if i.IsInstalled("v1.0.0") {
		t.Errorf("IsInstalled = true after failed install, want false")
	}
}

func TestInstallRemovesStalePartialThenSucceeds(t *testing.T) {
	i := newInstaller(t)

	// First attempt fails, leaving a stale partial.
	_, err := i.Install("v1.0.0", func(dir string) error {
		if werr := os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("old"), 0o644); werr != nil {
			return werr
		}
		return errors.New("first failure")
	})
	if err == nil {
		t.Fatalf("first Install: expected error")
	}
	mustExist(t, filepath.Join(i.partialDir("v1.0.0"), "stale.txt"))

	// Second attempt cleans the stale partial and promotes fresh content.
	res, err := i.Install("v1.0.0", func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new"), 0o644)
	})
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if !res.Installed {
		t.Errorf("Installed = false, want true")
	}

	mustExist(t, filepath.Join(i.ReleaseDir("v1.0.0"), "fresh.txt"))
	// The stale artifact must not have leaked into the promoted release.
	mustNotExist(t, filepath.Join(i.ReleaseDir("v1.0.0"), "stale.txt"))
	mustNotExist(t, i.partialDir("v1.0.0"))
}

func TestInstallAlreadyInstalledIsNoOp(t *testing.T) {
	i := newInstaller(t)

	if _, err := i.Install("v1.0.0", func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1"), 0o644)
	}); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	called := false
	res, err := i.Install("v1.0.0", func(dir string) error {
		called = true
		return os.WriteFile(filepath.Join(dir, "b.txt"), []byte("2"), 0o644)
	})
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if res.Installed {
		t.Errorf("Installed = true, want false for no-op")
	}
	if called {
		t.Errorf("stage func was called for an already-installed release")
	}
	// The original content is intact; the no-op did not re-stage.
	mustExist(t, filepath.Join(i.ReleaseDir("v1.0.0"), "a.txt"))
	mustNotExist(t, filepath.Join(i.ReleaseDir("v1.0.0"), "b.txt"))
}

func TestInstallUnmarkedExists(t *testing.T) {
	i := newInstaller(t)

	// Create a release directory without a marker (simulating a leftover or
	// externally created directory).
	if err := os.MkdirAll(i.ReleaseDir("v1.0.0"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	called := false
	_, err := i.Install("v1.0.0", func(dir string) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrUnmarkedExists) {
		t.Fatalf("Install err = %v, want ErrUnmarkedExists", err)
	}
	if called {
		t.Errorf("stage func was called despite unmarked existing directory")
	}
}

func TestInstallNilStage(t *testing.T) {
	i := newInstaller(t)
	if _, err := i.Install("v1.0.0", nil); err == nil {
		t.Fatalf("Install with nil stage: expected error")
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, markerName)
	want := InstalledMarker{
		Release:       "v2.3.4",
		InstalledAt:   time.Date(2026, 5, 31, 9, 0, 0, 0, time.UTC),
		SchemaVersion: markerSchemaVersion,
	}
	if err := writeMarker(path, want); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}
	// The temporary file must not be left behind.
	mustNotExist(t, path+".tmp")

	got, err := readMarker(path)
	if err != nil {
		t.Fatalf("readMarker: %v", err)
	}
	if got.Release != want.Release || got.SchemaVersion != want.SchemaVersion || !got.InstalledAt.Equal(want.InstalledAt) {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestValidateRelease(t *testing.T) {
	tests := []struct {
		name    string
		release string
		wantErr bool
	}{
		{"simple", "v1.0.0", false},
		{"alnum", "release2024", false},
		{"underscore-dash-dot", "v1_2-3.4", false},
		{"empty", "", true},
		{"slash", "a/b", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"leading-dot", ".hidden", true},
		{"traversal", "../escape", true},
		{"dollar", "v1$", true},
		{"space", "v 1", true},
		{"leading-dash", "-v1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelease(tt.release)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateRelease(%q) = nil, want error", tt.release)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateRelease(%q) = %v, want nil", tt.release, err)
			}
		})
	}
}

func TestInstallInvalidRelease(t *testing.T) {
	i := newInstaller(t)
	_, err := i.Install("../escape", func(string) error { return nil })
	if !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("Install err = %v, want ErrInvalidRelease", err)
	}
}
