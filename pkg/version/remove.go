package version

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Remove uninstalls an installed release. It validates the release name and
// requires the release to be installed. The release directory is checked with
// the same safety guard used by the install state machine before anything is
// deleted. When the current symlink resolves to the release being removed, the
// symlink is cleared first so no dangling active release is left behind.
func (i *Installer) Remove(release string) error {
	if err := ValidateRelease(release); err != nil {
		return err
	}
	if !i.IsInstalled(release) {
		return fmt.Errorf("%w: %q", ErrNotInstalled, release)
	}

	releaseDir := i.ReleaseDir(release)
	if err := i.guardManagedDir(releaseDir, release); err != nil {
		return err
	}

	active, err := i.currentPointsAt(releaseDir)
	if err != nil {
		return err
	}
	if active {
		if err := os.Remove(i.CurrentLink()); err != nil {
			return fmt.Errorf("version: clear current link: %w", err)
		}
	}

	if err := os.RemoveAll(releaseDir); err != nil {
		return fmt.Errorf("version: remove release %q: %w", release, err)
	}
	return nil
}

// currentPointsAt reports whether the current symlink resolves to releaseDir.
// Relative link targets are resolved against the link's directory. A missing
// current link, or a current path that is not a symlink, reports false without
// error so the existing path is left untouched; other errors (for example
// permission or I/O failures) are propagated so removal does not silently leave
// a dangling active release behind.
func (i *Installer) currentPointsAt(releaseDir string) (bool, error) {
	target, err := os.Readlink(i.CurrentLink())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EINVAL) {
			return false, nil
		}
		return false, fmt.Errorf("version: read current link: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(i.CurrentLink()), target)
	}
	absRelease, err := filepath.Abs(releaseDir)
	if err != nil {
		return false, fmt.Errorf("version: resolve release dir: %w", err)
	}
	return filepath.Clean(target) == filepath.Clean(absRelease), nil
}
