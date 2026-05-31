package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListReturnsInstalledReleasesSorted(t *testing.T) {
	root := t.TempDir()
	inst := &Installer{Root: root}

	// Two fully installed releases.
	for _, rel := range []string{"2026.05.16", "2026.04.01"} {
		if _, err := inst.Install(rel, func(string) error { return nil }); err != nil {
			t.Fatalf("Install(%s) error = %v", rel, err)
		}
	}

	// An unmarked directory (should be skipped).
	if err := os.MkdirAll(filepath.Join(inst.VersionsDir(), "2026.01.01"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A dotted staging directory (should be skipped).
	if err := os.MkdirAll(filepath.Join(inst.VersionsDir(), ".2026.05.16.partial"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := inst.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []string{"2026.04.01", "2026.05.16"}
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestListEmptyWhenNoVersionsDir(t *testing.T) {
	inst := &Installer{Root: t.TempDir()}
	got, err := inst.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got != nil {
		t.Errorf("List() = %v, want nil", got)
	}
}
