// Package version implements release version management for the Tenstorrent
// stack environment, including the atomic install state machine that stages a
// release under a per-release .partial directory and promotes it to its final
// location with a single atomic rename.
//
// Concurrency: the installer assumes a single writer per TT_HOME. Concurrent
// installs of the same release are not guarded with locking in this milestone
// and may race on the shared .partial directory.
package version

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// markerName is the basename of the status marker written into a release
// directory once the release has been fully installed and promoted.
const markerName = ".tt-env-installed"

// markerSchemaVersion is the current schema version of the installed marker.
// It is incremented when the marker's on-disk format changes.
const markerSchemaVersion = 1

// releaseNameRe restricts release names to a safe, path-component-only subset.
// It mirrors the proto1 grammar and prevents path traversal or separators.
var releaseNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ErrUnmarkedExists is returned when a release directory exists but does not
// contain a valid installed marker. Recreating such a directory requires the
// force-reinstall flow handled separately.
var ErrUnmarkedExists = errors.New("version: release directory exists but is not marked installed")

// ErrInvalidRelease is returned when a release name fails validation.
var ErrInvalidRelease = errors.New("version: invalid release name")

// InstalledMarker captures the metadata recorded in the .tt-env-installed
// status file when a release is promoted.
type InstalledMarker struct {
	Release       string    `json:"release"`
	InstalledAt   time.Time `json:"installed_at"`
	SchemaVersion int       `json:"schema_version"`
}

// Result describes the outcome of an Install call.
type Result struct {
	// Release is the validated release name.
	Release string
	// Path is the final release directory.
	Path string
	// Installed reports whether a new install was performed. It is false when
	// the release was already installed and the call was a no-op.
	Installed bool
}

// InstallFunc stages all install work for a release into stagingDir. The
// staging directory is created before the function is called. Returning a
// non-nil error aborts promotion and leaves the staging directory in place for
// inspection; it is cleaned up automatically on the next install attempt.
type InstallFunc func(stagingDir string) error

// Installer manages release installation under a TT_HOME root directory.
type Installer struct {
	// Root is the TT_HOME directory under which the versions/ tree lives.
	Root string
	// now returns the current time. It is overridable in tests; when nil,
	// time.Now is used.
	now func() time.Time
}

// ValidateRelease reports whether release is a syntactically valid release
// name. Valid names are single path components matching releaseNameRe and are
// never "." or "..".
func ValidateRelease(release string) error {
	if !releaseNameRe.MatchString(release) || release == "." || release == ".." {
		return fmt.Errorf("%w: %q", ErrInvalidRelease, release)
	}
	return nil
}

// VersionsDir returns the directory holding all installed release directories.
func (i *Installer) VersionsDir() string {
	return filepath.Join(i.Root, "versions")
}

// ReleaseDir returns the final directory for the given release.
func (i *Installer) ReleaseDir(release string) string {
	return filepath.Join(i.VersionsDir(), release)
}

// partialDir returns the staging directory for the given release.
func (i *Installer) partialDir(release string) string {
	return filepath.Join(i.VersionsDir(), "."+release+".partial")
}

// IsInstalled reports whether release has a fully promoted install, i.e. its
// release directory exists and contains a readable installed marker.
func (i *Installer) IsInstalled(release string) bool {
	if err := ValidateRelease(release); err != nil {
		return false
	}
	_, err := i.Marker(release)
	return err == nil
}

// Marker reads and returns the installed marker for release. It returns an
// error if the release is not installed or the marker cannot be parsed.
func (i *Installer) Marker(release string) (InstalledMarker, error) {
	if err := ValidateRelease(release); err != nil {
		return InstalledMarker{}, err
	}
	return readMarker(filepath.Join(i.ReleaseDir(release), markerName))
}

func (i *Installer) clock() time.Time {
	if i.now != nil {
		return i.now()
	}
	return time.Now()
}

// Install stages a release under a .partial directory and, on success,
// promotes it to its final location with an atomic rename and records the
// installed marker.
//
// If the release is already installed, Install is a no-op and returns a Result
// with Installed set to false. If the release directory exists but is not
// marked installed, Install returns ErrUnmarkedExists without modifying it.
//
// On a staging failure the partial directory is left in place (not promoted)
// so the previous state is untouched; the stale partial is removed on the next
// install attempt.
func (i *Installer) Install(release string, stage InstallFunc) (Result, error) {
	if err := ValidateRelease(release); err != nil {
		return Result{}, err
	}
	if stage == nil {
		return Result{}, errors.New("version: stage function must not be nil")
	}

	versionsDir := i.VersionsDir()
	releaseDir := i.ReleaseDir(release)
	partialDir := i.partialDir(release)

	switch _, err := os.Stat(releaseDir); {
	case err == nil:
		if _, mErr := i.Marker(release); mErr == nil {
			return Result{Release: release, Path: releaseDir, Installed: false}, nil
		}
		return Result{}, fmt.Errorf("%w: %s", ErrUnmarkedExists, releaseDir)
	case !errors.Is(err, os.ErrNotExist):
		return Result{}, fmt.Errorf("version: stat release directory: %w", err)
	}

	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("version: create versions directory: %w", err)
	}

	if err := i.removeStalePartial(release); err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(partialDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("version: create partial directory: %w", err)
	}

	if err := stage(partialDir); err != nil {
		return Result{}, fmt.Errorf("version: stage release %q: %w", release, err)
	}

	if err := writeMarker(filepath.Join(partialDir, markerName), InstalledMarker{
		Release:       release,
		InstalledAt:   i.clock().UTC(),
		SchemaVersion: markerSchemaVersion,
	}); err != nil {
		return Result{}, err
	}

	if err := os.Rename(partialDir, releaseDir); err != nil {
		return Result{}, fmt.Errorf("version: promote release %q: %w", release, err)
	}

	return Result{Release: release, Path: releaseDir, Installed: true}, nil
}

// removeStalePartial removes a leftover partial directory from a previous
// interrupted install. It refuses to remove any path that is not the expected
// partial directory directly under versionsDir, guarding against accidental
// destructive removal.
func (i *Installer) removeStalePartial(release string) error {
	partialDir := i.partialDir(release)

	info, err := os.Lstat(partialDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("version: stat partial directory: %w", err)
	}

	wantBase := "." + release + ".partial"
	if filepath.Base(partialDir) != wantBase ||
		filepath.Clean(filepath.Dir(partialDir)) != filepath.Clean(i.VersionsDir()) {
		return fmt.Errorf("version: refusing to remove unsafe partial directory: %s", partialDir)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("version: refusing to remove partial symlink: %s", partialDir)
	}

	if err := os.RemoveAll(partialDir); err != nil {
		return fmt.Errorf("version: remove stale partial directory: %w", err)
	}
	return nil
}

// writeMarker writes the marker JSON atomically by writing to a temporary file
// in the same directory and renaming it into place.
func writeMarker(path string, m InstalledMarker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("version: marshal marker: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("version: write marker: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("version: finalize marker: %w", err)
	}
	return nil
}

// readMarker reads and parses an installed marker from path.
func readMarker(path string) (InstalledMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return InstalledMarker{}, fmt.Errorf("version: read marker: %w", err)
	}
	var m InstalledMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return InstalledMarker{}, fmt.Errorf("version: parse marker %s: %w", path, err)
	}
	return m, nil
}
