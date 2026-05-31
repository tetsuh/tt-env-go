package version

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrNotInstalled indicates that an operation targeted a release that is not
// installed (its release directory is missing or unmarked).
var ErrNotInstalled = errors.New("version: release is not installed")

// CurrentLink returns the path of the active-release symlink (Root/current).
func (i *Installer) CurrentLink() string {
	return filepath.Join(i.Root, "current")
}

// Use atomically points the current symlink at the given installed release.
//
// The switch is performed by creating a temporary symlink and renaming it over
// the existing current link, so the link is never left in a broken intermediate
// state. Use validates that the target release is installed before touching the
// link, so switching to a non-installed release fails without changing current.
// It refuses to replace an existing current path that is not a symlink.
func (i *Installer) Use(release string) error {
	if err := ValidateRelease(release); err != nil {
		return err
	}
	if !i.IsInstalled(release) {
		return fmt.Errorf("%w: %q", ErrNotInstalled, release)
	}

	releaseDir := i.ReleaseDir(release)
	target, err := filepath.Abs(releaseDir)
	if err != nil {
		return fmt.Errorf("version: resolve release dir: %w", err)
	}
	link := i.CurrentLink()

	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("version: refusing to replace non-symlink current path: %s", link)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("version: stat current link: %w", err)
	}

	if err := os.MkdirAll(i.Root, 0o755); err != nil {
		return fmt.Errorf("version: create root: %w", err)
	}

	// Create the symlink under a unique hidden temp name and atomically rename
	// it over current, so the link is never left broken mid-switch.
	tmp := filepath.Join(i.Root, fmt.Sprintf(".current.%d.tmp", time.Now().UnixNano()))
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("version: create temp current link: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("version: activate current link: %w", err)
	}
	return nil
}

// Current returns the release name that the current symlink points at. It
// returns an error if the link is missing, unreadable, or points outside the
// versions directory.
func (i *Installer) Current() (string, error) {
	link := i.CurrentLink()
	target, err := os.Readlink(link)
	if err != nil {
		return "", fmt.Errorf("version: read current link: %w", err)
	}
	if filepath.Clean(filepath.Dir(target)) != filepath.Clean(i.VersionsDir()) {
		return "", fmt.Errorf("version: current link points outside versions dir: %s", target)
	}
	release := filepath.Base(target)
	if err := ValidateRelease(release); err != nil {
		return "", err
	}
	return release, nil
}
