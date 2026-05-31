// Package version implements release version management for the Tenstorrent
// stack environment, including the atomic install state machine that stages a
// release under a per-release .partial directory and promotes it to its final
// location with a single atomic rename.
//
// A forced reinstall replaces an existing release by swapping it through a
// per-release .backup directory so the previous release can be restored if the
// final rename fails. Install repairs leftover .partial and .backup state from
// an interrupted run before proceeding.
//
// Concurrency: the installer assumes a single writer per TT_HOME. Concurrent
// installs of the same release are not guarded with locking in this milestone
// and may race on the shared .partial and .backup directories.
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
	// Replaced reports whether a previously present release directory was
	// replaced by this install. It is only true for forced reinstalls.
	Replaced bool
}

// Option configures an Install call.
type Option func(*installConfig)

type installConfig struct {
	force bool
}

// WithForce enables force-reinstall: when set, Install reinstalls a release
// even if it is already present, replacing the existing release directory.
func WithForce(force bool) Option {
	return func(c *installConfig) { c.force = force }
}

// InstallFunc stages all install work for a release into stagingDir. The
// staging directory is created before the function is called. Returning a
// non-nil error aborts promotion; the staging directory is then rolled back
// (removed) and any previously installed release is left intact.
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

// backupDir returns the directory used to hold a previous release while a
// forced reinstall swaps in the freshly staged one.
func (i *Installer) backupDir(release string) string {
	return filepath.Join(i.VersionsDir(), "."+release+".backup")
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
// with Installed set to false, unless WithForce(true) is supplied, in which
// case the existing release is reinstalled (Replaced set to true). If the
// release directory exists but is not marked installed, Install returns
// ErrUnmarkedExists unless WithForce(true) is supplied.
//
// On any failure after staging begins, the partial directory is rolled back
// (removed) and any previously installed release is left intact: the existing
// release is only touched after staging and the marker write both succeed.
func (i *Installer) Install(release string, stage InstallFunc, opts ...Option) (Result, error) {
	if err := ValidateRelease(release); err != nil {
		return Result{}, err
	}
	if stage == nil {
		return Result{}, errors.New("version: stage function must not be nil")
	}

	var cfg installConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	versionsDir := i.VersionsDir()
	releaseDir := i.ReleaseDir(release)
	partialDir := i.partialDir(release)

	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("version: create versions directory: %w", err)
	}

	// Recover from a previously interrupted forced reinstall before inspecting
	// the release directory, so a release stranded in its backup is restored.
	if err := i.recoverBackup(release); err != nil {
		return Result{}, err
	}

	replacing := false
	switch _, err := os.Stat(releaseDir); {
	case err == nil:
		if _, mErr := i.Marker(release); mErr == nil {
			if !cfg.force {
				return Result{Release: release, Path: releaseDir, Installed: false}, nil
			}
		} else if !cfg.force {
			return Result{}, fmt.Errorf("%w: %s", ErrUnmarkedExists, releaseDir)
		}
		replacing = true
	case !errors.Is(err, os.ErrNotExist):
		return Result{}, fmt.Errorf("version: stat release directory: %w", err)
	}

	if err := i.removeStalePartial(release); err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(partialDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("version: create partial directory: %w", err)
	}

	if err := stage(partialDir); err != nil {
		return Result{}, i.rollback(partialDir, fmt.Errorf("version: stage release %q: %w", release, err))
	}

	if err := writeMarker(filepath.Join(partialDir, markerName), InstalledMarker{
		Release:       release,
		InstalledAt:   i.clock().UTC(),
		SchemaVersion: markerSchemaVersion,
	}); err != nil {
		return Result{}, i.rollback(partialDir, err)
	}

	if err := i.promote(release, replacing); err != nil {
		return Result{}, err
	}

	return Result{Release: release, Path: releaseDir, Installed: true, Replaced: replacing}, nil
}

// rollback removes a partial directory after a failed install and returns the
// original cause, joined with any cleanup error so neither is hidden.
func (i *Installer) rollback(partialDir string, cause error) error {
	if err := os.RemoveAll(partialDir); err != nil {
		return errors.Join(cause, fmt.Errorf("version: rollback partial directory: %w", err))
	}
	return cause
}

// promote atomically moves the staged partial directory into the final release
// location. When replacing an existing release it swaps via a backup directory
// so the previous release can be restored if the final rename fails.
func (i *Installer) promote(release string, replacing bool) error {
	partialDir := i.partialDir(release)
	releaseDir := i.ReleaseDir(release)

	if !replacing {
		if err := os.Rename(partialDir, releaseDir); err != nil {
			return fmt.Errorf("version: promote release %q: %w", release, err)
		}
		return nil
	}

	backupDir := i.backupDir(release)
	if err := i.guardManagedDir(backupDir, "."+release+".backup"); err != nil {
		return err
	}
	if err := os.Rename(releaseDir, backupDir); err != nil {
		return fmt.Errorf("version: back up existing release %q: %w", release, err)
	}
	if err := os.Rename(partialDir, releaseDir); err != nil {
		// Restore the previous release so it stays usable.
		if rErr := os.Rename(backupDir, releaseDir); rErr != nil {
			return errors.Join(
				fmt.Errorf("version: promote release %q: %w", release, err),
				fmt.Errorf("version: restore previous release %q: %w", release, rErr),
			)
		}
		return fmt.Errorf("version: promote release %q: %w", release, err)
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("version: remove backup of release %q: %w", release, err)
	}
	return nil
}

// recoverBackup repairs state left by an interrupted forced reinstall. If a
// backup directory exists, it is either restored (when the release directory is
// missing because a crash happened mid-swap) or removed (when the swap had
// already completed but backup cleanup did not).
func (i *Installer) recoverBackup(release string) error {
	backupDir := i.backupDir(release)
	if _, err := os.Lstat(backupDir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("version: stat backup directory: %w", err)
	}
	if err := i.guardManagedDir(backupDir, "."+release+".backup"); err != nil {
		return err
	}

	if _, err := os.Stat(i.ReleaseDir(release)); errors.Is(err, os.ErrNotExist) {
		if rErr := os.Rename(backupDir, i.ReleaseDir(release)); rErr != nil {
			return fmt.Errorf("version: restore backup of release %q: %w", release, rErr)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("version: stat release directory: %w", err)
	}

	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("version: remove stale backup directory: %w", err)
	}
	return nil
}

// removeStalePartial removes a leftover partial directory from a previous
// interrupted install, guarding against accidental destructive removal.
func (i *Installer) removeStalePartial(release string) error {
	partialDir := i.partialDir(release)

	if _, err := os.Lstat(partialDir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("version: stat partial directory: %w", err)
	}
	if err := i.guardManagedDir(partialDir, "."+release+".partial"); err != nil {
		return err
	}
	if err := os.RemoveAll(partialDir); err != nil {
		return fmt.Errorf("version: remove stale partial directory: %w", err)
	}
	return nil
}

// guardManagedDir verifies that path is the expected managed directory directly
// under versionsDir and is not a symlink, before it is removed or renamed.
func (i *Installer) guardManagedDir(path, wantBase string) error {
	if filepath.Base(path) != wantBase ||
		filepath.Clean(filepath.Dir(path)) != filepath.Clean(i.VersionsDir()) {
		return fmt.Errorf("version: refusing to manage unsafe directory: %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("version: stat managed directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("version: refusing to manage symlink: %s", path)
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
		_ = os.Remove(tmp)
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
