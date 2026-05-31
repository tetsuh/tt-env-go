package version

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// installForTest installs a minimal release so it is marked installed.
func installForTest(t *testing.T, i *Installer, release string) {
	t.Helper()
	if _, err := i.Install(release, func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("data"), 0o644)
	}); err != nil {
		t.Fatalf("Install(%s): %v", release, err)
	}
}

func readCurrentTarget(t *testing.T, i *Installer) string {
	t.Helper()
	target, err := os.Readlink(i.CurrentLink())
	if err != nil {
		t.Fatalf("readlink current: %v", err)
	}
	return target
}

func TestUseSwitchesCurrent(t *testing.T) {
	i := newInstaller(t)
	installForTest(t, i, "v1.0.0")

	if err := i.Use("v1.0.0"); err != nil {
		t.Fatalf("Use: %v", err)
	}

	if got := readCurrentTarget(t, i); got != i.ReleaseDir("v1.0.0") {
		t.Errorf("current target = %q, want %q", got, i.ReleaseDir("v1.0.0"))
	}
	cur, err := i.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur != "v1.0.0" {
		t.Errorf("Current = %q, want v1.0.0", cur)
	}
	// The link resolves to the real release directory contents.
	mustExist(t, filepath.Join(i.CurrentLink(), "artifact.txt"))
}

func TestUseReSwitch(t *testing.T) {
	i := newInstaller(t)
	installForTest(t, i, "v1.0.0")
	installForTest(t, i, "v2.0.0")

	if err := i.Use("v1.0.0"); err != nil {
		t.Fatalf("Use v1: %v", err)
	}
	if err := i.Use("v2.0.0"); err != nil {
		t.Fatalf("Use v2: %v", err)
	}

	if got := readCurrentTarget(t, i); got != i.ReleaseDir("v2.0.0") {
		t.Errorf("current target = %q, want %q", got, i.ReleaseDir("v2.0.0"))
	}
	cur, err := i.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur != "v2.0.0" {
		t.Errorf("Current = %q, want v2.0.0", cur)
	}
	// No leftover temp link.
	matches, _ := filepath.Glob(filepath.Join(i.Root, ".current.*.tmp"))
	if len(matches) != 0 {
		t.Errorf("leftover temp links: %v", matches)
	}
}

func TestUseReSwitchToSameRelease(t *testing.T) {
	i := newInstaller(t)
	installForTest(t, i, "v1.0.0")

	if err := i.Use("v1.0.0"); err != nil {
		t.Fatalf("Use first: %v", err)
	}
	if err := i.Use("v1.0.0"); err != nil {
		t.Fatalf("Use again: %v", err)
	}
	if got := readCurrentTarget(t, i); got != i.ReleaseDir("v1.0.0") {
		t.Errorf("current target = %q, want %q", got, i.ReleaseDir("v1.0.0"))
	}
}

func TestUseMissingReleaseFails(t *testing.T) {
	i := newInstaller(t)

	err := i.Use("v9.9.9")
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Use err = %v, want ErrNotInstalled", err)
	}
	mustNotExist(t, i.CurrentLink())
}

func TestUseMissingReleaseLeavesCurrentUnchanged(t *testing.T) {
	i := newInstaller(t)
	installForTest(t, i, "v1.0.0")
	if err := i.Use("v1.0.0"); err != nil {
		t.Fatalf("Use v1: %v", err)
	}

	if err := i.Use("v2.0.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Use v2 err = %v, want ErrNotInstalled", err)
	}
	// current still points at v1.
	if got := readCurrentTarget(t, i); got != i.ReleaseDir("v1.0.0") {
		t.Errorf("current target = %q, want %q (unchanged)", got, i.ReleaseDir("v1.0.0"))
	}
}

func TestUseUnmarkedReleaseFails(t *testing.T) {
	i := newInstaller(t)
	// Create a release directory without the installed marker.
	if err := os.MkdirAll(i.ReleaseDir("v1.0.0"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := i.Use("v1.0.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Use err = %v, want ErrNotInstalled", err)
	}
	mustNotExist(t, i.CurrentLink())
}

func TestUseInvalidRelease(t *testing.T) {
	i := newInstaller(t)
	if err := i.Use("../evil"); !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("Use err = %v, want ErrInvalidRelease", err)
	}
}

func TestUseRefusesNonSymlinkCurrent(t *testing.T) {
	i := newInstaller(t)
	installForTest(t, i, "v1.0.0")
	// Pre-create current as a real directory.
	if err := os.MkdirAll(i.CurrentLink(), 0o755); err != nil {
		t.Fatalf("mkdir current: %v", err)
	}

	if err := i.Use("v1.0.0"); err == nil {
		t.Fatalf("Use: expected error replacing non-symlink current")
	}
	// The directory is preserved.
	info, err := os.Lstat(i.CurrentLink())
	if err != nil {
		t.Fatalf("lstat current: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("current should remain a directory")
	}
}

func TestUseAndCurrentWithRelativeRoot(t *testing.T) {
	// Use() writes an absolute symlink target; Current() must resolve
	// VersionsDir to absolute so the comparison holds for a relative Root.
	base := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	i := &Installer{Root: "tt-home"}
	installForTest(t, i, "v1.0.0")

	if err := i.Use("v1.0.0"); err != nil {
		t.Fatalf("Use: %v", err)
	}
	cur, err := i.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur != "v1.0.0" {
		t.Errorf("Current = %q, want v1.0.0", cur)
	}
}

func TestCurrentMissingLink(t *testing.T) {
	i := newInstaller(t)
	if _, err := i.Current(); err == nil {
		t.Fatalf("Current: expected error when link is missing")
	}
}

func TestCurrentPointsOutsideVersions(t *testing.T) {
	i := newInstaller(t)
	if err := os.MkdirAll(i.Root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.Symlink("/etc", i.CurrentLink()); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := i.Current(); err == nil {
		t.Fatalf("Current: expected error for target outside versions dir")
	}
}
