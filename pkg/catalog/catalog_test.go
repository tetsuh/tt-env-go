package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPathPrefersLocalOverCatalog(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", "0.75.0.json"), `{"release":"0.75.0"}`)
	mustWrite(t, filepath.Join(root, "releases.local", "0.75.0.json"), `{"release":"0.75.0","description":"local override"}`)

	path, local, err := Path(root, "0.75.0")
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if !local {
		t.Error("Path() local = false, want true when both locations define the release")
	}
	if want := filepath.Join(root, "releases.local", "0.75.0.json"); path != want {
		t.Errorf("Path() = %q, want %q", path, want)
	}
}

func TestPathFallsBackToCatalog(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", "0.75.0.json"), `{"release":"0.75.0"}`)

	path, local, err := Path(root, "0.75.0")
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if local {
		t.Error("Path() local = true, want false for a catalog-only release")
	}
	if want := filepath.Join(root, "releases", "0.75.0.json"); path != want {
		t.Errorf("Path() = %q, want %q", path, want)
	}
}

func TestPathMissingRelease(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Path(root, "missing"); err == nil {
		t.Fatal("Path() should fail when no manifest exists in either location")
	}
}

func TestPathRejectsTraversalNames(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", "0.75.0.json"), `{"release":"0.75.0"}`)

	for _, name := range []string{"..", ".", "a/b", `a\b`, ""} {
		if _, _, err := Path(root, name); err == nil {
			t.Errorf("Path(%q) should reject the name", name)
		}
	}
}

func TestAvailableMergesBothLocations(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", "a.json"), `{"release":"0.75.0"}`)
	mustWrite(t, filepath.Join(root, "releases", "b.json"), `{"release":"0.76.0"}`)
	mustWrite(t, filepath.Join(root, "releases.local", "c.json"), `{"release":"2026.05.16"}`)
	// A local manifest shadowing a catalog release with the same name.
	mustWrite(t, filepath.Join(root, "releases.local", "b.json"), `{"release":"0.76.0"}`)
	mustWrite(t, filepath.Join(root, "releases", "bad.json"), `{invalid`)

	entries, warnings := Available(root)
	want := []Entry{
		{Release: "0.75.0"},
		{Release: "0.76.0", Local: true},
		{Release: "2026.05.16", Local: true},
	}
	if len(entries) != len(want) {
		t.Fatalf("Available() = %v, want %v", entries, want)
	}
	for i, e := range entries {
		if e != want[i] {
			t.Errorf("Available()[%d] = %+v, want %+v", i, e, want[i])
		}
	}
	if len(warnings) != 1 || filepath.Base(warnings[0]) != "bad.json" {
		t.Errorf("warnings = %v, want [bad.json]", warnings)
	}
}

func TestAvailableEmptyWhenDirectoriesMissing(t *testing.T) {
	entries, warnings := Available(t.TempDir())
	if len(entries) != 0 || len(warnings) != 0 {
		t.Errorf("Available() = %v/%v, want empty", entries, warnings)
	}
}
