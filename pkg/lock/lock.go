// Package lock defines the install-time lock: the resolved, concrete manifest
// recorded at versions/<release>/manifest.json when a release is installed.
//
// A catalog manifest states intent (which packages and versions an install
// should resolve); the lock records resolution — the concrete versions that
// were actually installed, plus provenance describing where the plan came from
// (catalog, local manifest, or a --latest install and its structural base).
// The lock is written during staging, before promotion, so a release tree is
// either fully installed with its lock or not installed at all.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// FileName is the basename of the lock file inside a release directory.
const FileName = "manifest.json"

// Source values recorded in a lock's "source" field.
const (
	// SourceCatalog marks a pinned install whose plan came from the catalog
	// cache (releases/).
	SourceCatalog = "catalog"
	// SourceLocal marks a pinned install whose plan came from a local manifest
	// (releases.local/).
	SourceLocal = "local"
	// SourceLatest marks a --latest install: unpinned system and Python
	// packages and git components at their remote HEAD, seeded by "base".
	SourceLatest = "latest"
)

// ErrNotLocked is returned when a release directory has no lock manifest.
var ErrNotLocked = errors.New("lock: release has no lock manifest")

// Lock is the resolved manifest plus provenance, written to
// versions/<release>/manifest.json at install time. The embedded manifest is
// the fully resolved form: concrete system-package and pip versions, git
// revisions, and the container components as installed.
type Lock struct {
	manifest.Manifest
	// Source records where the install plan came from.
	Source string `json:"source"`
	// Base names the release whose structure seeded a --latest install.
	Base string `json:"base,omitempty"`
	// CatalogRepo and CatalogRef cite the catalog repository and ref the plan
	// manifest was fetched from, when known.
	CatalogRepo string `json:"catalog_repo,omitempty"`
	CatalogRef  string `json:"catalog_ref,omitempty"`
	// InstalledAt is when the install was promoted.
	InstalledAt time.Time `json:"installed_at"`
}

// Path returns the lock file path inside the given release directory.
func Path(releaseDir string) string {
	return filepath.Join(releaseDir, FileName)
}

// Write validates l and atomically writes it into releaseDir, so a reader
// never sees a partial lock.
func Write(releaseDir string, l *Lock) error {
	if err := l.Validate(); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("lock: marshal lock: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return fmt.Errorf("lock: create release directory: %w", err)
	}
	tmp, err := os.CreateTemp(releaseDir, ".lock-*.json")
	if err != nil {
		return fmt.Errorf("lock: create temp lock: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("lock: write temp lock: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("lock: chmod temp lock: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("lock: sync temp lock: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("lock: close temp lock: %w", err)
	}
	if err := os.Rename(tmpName, Path(releaseDir)); err != nil {
		return fmt.Errorf("lock: write lock %s: %w", Path(releaseDir), err)
	}
	return nil
}

// Read loads and validates the lock from releaseDir. It returns an error
// wrapping ErrNotLocked when the release has no lock (for example, it was
// installed before locks existed).
func Read(releaseDir string) (*Lock, error) {
	data, err := os.ReadFile(Path(releaseDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotLocked, Path(releaseDir))
		}
		return nil, fmt.Errorf("lock: read lock: %w", err)
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("lock: parse lock %s: %w", Path(releaseDir), err)
	}
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("lock: %s: %w", Path(releaseDir), err)
	}
	return &l, nil
}

// Validate checks the resolved manifest and the provenance fields.
func (l *Lock) Validate() error {
	if err := l.Manifest.Validate(); err != nil {
		return err
	}
	switch l.Source {
	case SourceCatalog, SourceLocal, SourceLatest:
	default:
		return fmt.Errorf("invalid source %q (want %q, %q, or %q)", l.Source, SourceCatalog, SourceLocal, SourceLatest)
	}
	if l.Base != "" {
		if err := version.ValidateRelease(l.Base); err != nil {
			return fmt.Errorf("invalid base: %w", err)
		}
	}
	return nil
}

// Describe renders a short human-readable provenance summary, e.g.
// "from catalog tetsuh/tt-env-manifests@main, resolved 2026-09-22" or
// "latest of base 2026.05.16, resolved 2026-09-22". It is used by list and
// status output.
func (l *Lock) Describe() string {
	date := ""
	if !l.InstalledAt.IsZero() {
		date = ", resolved " + l.InstalledAt.Format("2006-01-02")
	}
	switch l.Source {
	case SourceLatest:
		if l.Base != "" {
			return fmt.Sprintf("latest of base %s%s", l.Base, date)
		}
		return "latest" + date
	case SourceLocal:
		return "from local manifest" + date
	default:
		if l.CatalogRepo != "" {
			ref := l.CatalogRef
			if ref == "" {
				ref = "?"
			}
			return fmt.Sprintf("from catalog %s@%s%s", l.CatalogRepo, ref, date)
		}
		return "from catalog" + date
	}
}
