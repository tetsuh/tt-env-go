package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
)

func TestWriteReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	l := &Lock{
		Manifest: manifest.Manifest{
			Release:        "2026.05.16",
			SystemPackages: map[string]string{"kmd": "1.0.0"},
			PythonPackages: map[string]string{"tt-smi": "1.1.0"},
		},
		Source:      SourceCatalog,
		CatalogRepo: "tetsuh/tt-env-manifests",
		CatalogRef:  "main",
		InstalledAt: at,
	}
	if err := Write(dir, l); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Release != "2026.05.16" || got.Source != SourceCatalog {
		t.Errorf("Read() = %+v", got)
	}
	if got.SystemPackages["kmd"] != "1.0.0" || got.PythonPackages["tt-smi"] != "1.1.0" {
		t.Errorf("resolved versions lost: %+v", got.Manifest)
	}
	if got.CatalogRepo != "tetsuh/tt-env-manifests" || got.CatalogRef != "main" {
		t.Errorf("catalog provenance lost: %+v", got)
	}
	if !got.InstalledAt.Equal(at) {
		t.Errorf("InstalledAt = %v, want %v", got.InstalledAt, at)
	}

	// A lock must remain a loadable, validating manifest.
	m, err := manifest.Load(Path(dir))
	if err != nil {
		t.Fatalf("manifest.Load(lock) error = %v", err)
	}
	if m.Release != "2026.05.16" || m.SystemPackages["kmd"] != "1.0.0" {
		t.Errorf("lock does not round-trip as a manifest: %+v", m)
	}
}

func TestReadMissingLock(t *testing.T) {
	_, err := Read(t.TempDir())
	if !errors.Is(err, ErrNotLocked) {
		t.Errorf("Read() error = %v, want ErrNotLocked", err)
	}
}

func TestReadRejectsInvalidLock(t *testing.T) {
	dir := t.TempDir()
	// A lock without a source is invalid.
	body := `{"release":"2026.05.16","system_packages":{"kmd":"1.0.0"}}`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "invalid source") {
		t.Errorf("Read() error = %v, want invalid source", err)
	}
}

func TestValidateRejectsBadBase(t *testing.T) {
	l := &Lock{
		Manifest:    manifest.Manifest{Release: "r"},
		Source:      SourceLatest,
		Base:        "../escape",
		InstalledAt: time.Now(),
	}
	if err := l.Validate(); err == nil || !strings.Contains(err.Error(), "invalid base") {
		t.Errorf("Validate() error = %v, want invalid base", err)
	}
}

func TestDescribeProvenance(t *testing.T) {
	at := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		l    Lock
		want string
	}{
		{"catalog with repo", Lock{Source: SourceCatalog, CatalogRepo: "tetsuh/tt-env-manifests", CatalogRef: "main", InstalledAt: at}, "from catalog tetsuh/tt-env-manifests@main, resolved 2026-09-23"},
		{"catalog without repo", Lock{Source: SourceCatalog, InstalledAt: at}, "from catalog, resolved 2026-09-23"},
		{"local", Lock{Source: SourceLocal, InstalledAt: at}, "from local manifest, resolved 2026-09-23"},
		{"latest with base", Lock{Source: SourceLatest, Base: "2026.05.16", InstalledAt: at}, "latest of base 2026.05.16, resolved 2026-09-23"},
		{"latest without date", Lock{Source: SourceLatest}, "latest"},
	}
	for _, tc := range cases {
		if got := tc.l.Describe(); got != tc.want {
			t.Errorf("%s: Describe() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, &Lock{Manifest: manifest.Manifest{Release: "r"}, Source: SourceLocal, InstalledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Errorf("dir entries = %v, want only %s", entries, FileName)
	}
	if info, err := os.Stat(filepath.Join(dir, FileName)); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("lock perms = %v err=%v, want 0644", info.Mode().Perm(), err)
	}
}
