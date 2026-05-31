package version

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveDeletesInstalledRelease(t *testing.T) {
	inst := &Installer{Root: t.TempDir()}
	if _, err := inst.Install("2026.05.16", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if err := inst.Remove("2026.05.16"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if inst.IsInstalled("2026.05.16") {
		t.Error("release should not be installed after Remove")
	}
	if _, err := os.Stat(inst.ReleaseDir("2026.05.16")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("release dir should be gone, stat err = %v", err)
	}
}

func TestRemoveClearsActiveSymlink(t *testing.T) {
	inst := &Installer{Root: t.TempDir()}
	if _, err := inst.Install("2026.05.16", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := inst.Use("2026.05.16"); err != nil {
		t.Fatal(err)
	}

	if err := inst.Remove("2026.05.16"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Lstat(inst.CurrentLink()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("current link should be cleared, lstat err = %v", err)
	}
}

func TestRemoveKeepsActiveSymlinkForOtherRelease(t *testing.T) {
	inst := &Installer{Root: t.TempDir()}
	for _, rel := range []string{"2026.04.01", "2026.05.16"} {
		if _, err := inst.Install(rel, func(string) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := inst.Use("2026.05.16"); err != nil {
		t.Fatal(err)
	}

	if err := inst.Remove("2026.04.01"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	active, err := inst.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if active != "2026.05.16" {
		t.Errorf("active release = %q, want unchanged 2026.05.16", active)
	}
}

func TestRemoveClearsActiveRelativeSymlink(t *testing.T) {
	root := t.TempDir()
	inst := &Installer{Root: root}
	if _, err := inst.Install("2026.05.16", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Simulate a proto1-style relative current symlink.
	if err := os.Symlink(filepath.Join("versions", "2026.05.16"), inst.CurrentLink()); err != nil {
		t.Fatal(err)
	}

	if err := inst.Remove("2026.05.16"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Lstat(inst.CurrentLink()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("relative current link should be cleared, lstat err = %v", err)
	}
}

func TestRemoveLeavesCurrentPointingElsewhere(t *testing.T) {
	root := t.TempDir()
	inst := &Installer{Root: root}
	for _, rel := range []string{"2026.04.01", "2026.05.16"} {
		if _, err := inst.Install(rel, func(string) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := inst.Use("2026.05.16"); err != nil {
		t.Fatal(err)
	}

	if err := inst.Remove("2026.04.01"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Lstat(inst.CurrentLink()); err != nil {
		t.Errorf("current link to a different release must be preserved, lstat err = %v", err)
	}
}

func TestRemoveRejectsUninstalledRelease(t *testing.T) {
	inst := &Installer{Root: t.TempDir()}
	err := inst.Remove("2026.05.16")
	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Remove() error = %v, want ErrNotInstalled", err)
	}
}

func TestRemoveRejectsInvalidReleaseName(t *testing.T) {
	inst := &Installer{Root: t.TempDir()}
	if err := inst.Remove("../escape"); !errors.Is(err, ErrInvalidRelease) {
		t.Errorf("Remove() error = %v, want ErrInvalidRelease", err)
	}
}
