// Package update ports the proto1 manifest updater: it fetches a release
// manifest archive from a configured GitHub source and atomically refreshes the
// ${TT_HOME}/releases and ${TT_HOME}/manifests directories.
//
// Binary self-update (proto1 "update --self") is intentionally not implemented;
// the Go build is distributed and updated through package managers or
// "go install".
package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo and DefaultRef are the manifest source used when the Updater does
// not override them, mirroring proto1 defaults.
const (
	DefaultRepo = "tetsuh/tt-env-manifests-proto1"
	DefaultRef  = "main"
)

var (
	repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	refRe  = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
)

// Fetcher retrieves the gzip-compressed tar archive for repo at ref, returning
// its raw bytes. token authenticates the request and must never be logged.
type Fetcher interface {
	Fetch(ctx context.Context, repo, ref, token string) ([]byte, error)
}

// Updater refreshes the local manifest cache under Root (TT_HOME).
type Updater struct {
	// Root is the TT_HOME directory whose releases/ and manifests/ are refreshed.
	Root string
	// Repo and Ref select the manifest source; empty values fall back to the
	// DefaultRepo and DefaultRef.
	Repo string
	Ref  string
	// Token authenticates the fetch and is required.
	Token string
	// Fetcher retrieves the manifest archive.
	Fetcher Fetcher
	// Now supplies the timestamp written to the update marker; defaults to
	// time.Now.
	Now func() time.Time
}

// Result summarizes a successful update.
type Result struct {
	Repo            string
	Ref             string
	ReleaseCount    int
	OSManifestCount int
}

// Update fetches the manifest archive and atomically replaces the local
// releases/ and manifests/ caches. It requires both directories to be present
// and non-empty in the archive, mirroring proto1 update_manifests.
func (u *Updater) Update(ctx context.Context) (Result, error) {
	repo := u.Repo
	if repo == "" {
		repo = DefaultRepo
	}
	ref := u.Ref
	if ref == "" {
		ref = DefaultRef
	}
	if err := validateSource(repo, ref); err != nil {
		return Result{}, err
	}
	if u.Token == "" {
		return Result{}, errors.New("update: authentication required: set GITHUB_TOKEN or run: gh auth login")
	}
	if u.Root == "" {
		return Result{}, errors.New("update: root directory must not be empty")
	}
	if u.Fetcher == nil {
		return Result{}, errors.New("update: no fetcher configured")
	}

	archive, err := u.Fetcher.Fetch(ctx, repo, ref, u.Token)
	if err != nil {
		return Result{}, err
	}

	set, err := extractManifests(archive)
	if err != nil {
		return Result{}, err
	}

	if err := u.apply(set); err != nil {
		return Result{}, err
	}

	return Result{
		Repo:            repo,
		Ref:             ref,
		ReleaseCount:    len(set.releases),
		OSManifestCount: len(set.osManifests),
	}, nil
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// apply stages the extracted manifests, writes the update marker, and swaps the
// new directories into place under a per-process lock.
func (u *Updater) apply(set *manifestSet) error {
	tmpRoot := filepath.Join(u.Root, ".tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return fmt.Errorf("update: create temp directory: %w", err)
	}

	lock := filepath.Join(tmpRoot, "update.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("update: another update is already in progress (remove %s if you are sure none is running)", lock)
		}
		return fmt.Errorf("update: acquire update lock: %w", err)
	}
	defer os.Remove(lock)

	work, err := os.MkdirTemp(tmpRoot, "update-")
	if err != nil {
		return fmt.Errorf("update: create staging directory: %w", err)
	}
	defer os.RemoveAll(work)

	stagingReleases := filepath.Join(work, "releases")
	stagingManifests := filepath.Join(work, "manifests")
	if err := writeFiles(stagingReleases, set.releases); err != nil {
		return err
	}
	if err := writeFiles(stagingManifests, set.osManifests); err != nil {
		return err
	}

	// Write the marker into the staged manifests directory so it lands together
	// with the swap; a separate post-swap write could fail after the swap.
	marker := strconv.FormatInt(u.now().Unix(), 10) + "\n"
	if err := os.WriteFile(filepath.Join(stagingManifests, "last_update"), []byte(marker), 0o644); err != nil {
		return fmt.Errorf("update: write update marker: %w", err)
	}

	backup := filepath.Join(work, "backup")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return fmt.Errorf("update: create backup directory: %w", err)
	}
	return u.swap(stagingReleases, stagingManifests, backup)
}

// swap replaces ${Root}/releases and ${Root}/manifests with the staged
// directories, rolling both back on any failure, mirroring proto1
// _update_apply_staged_manifests. The rollback is best-effort and, like proto1,
// is not crash-atomic: a process crash mid-swap can require manual recovery of
// the backup left under ${Root}/.tmp.
func (u *Updater) swap(stagingReleases, stagingManifests, backup string) error {
	finalReleases := filepath.Join(u.Root, "releases")
	finalManifests := filepath.Join(u.Root, "manifests")
	releasesBackup := filepath.Join(backup, "releases")
	manifestsBackup := filepath.Join(backup, "manifests")

	if err := moveAside(finalReleases, releasesBackup); err != nil {
		return fmt.Errorf("update: back up existing releases: %w", err)
	}
	if err := moveAside(finalManifests, manifestsBackup); err != nil {
		return errors.Join(
			fmt.Errorf("update: back up existing OS manifests: %w", err),
			restore(releasesBackup, finalReleases),
		)
	}
	if err := os.Rename(stagingReleases, finalReleases); err != nil {
		return errors.Join(
			fmt.Errorf("update: install releases: %w", err),
			restore(releasesBackup, finalReleases),
			restore(manifestsBackup, finalManifests),
		)
	}
	if err := os.Rename(stagingManifests, finalManifests); err != nil {
		return errors.Join(
			fmt.Errorf("update: install OS manifests: %w", err),
			os.RemoveAll(finalReleases),
			restore(releasesBackup, finalReleases),
			restore(manifestsBackup, finalManifests),
		)
	}
	return nil
}

// writeFiles creates dir and writes each named file into it.
func writeFiles(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("update: create %s: %w", dir, err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return fmt.Errorf("update: write %s: %w", name, err)
		}
	}
	return nil
}

// moveAside renames src to dst, treating a missing src as a no-op.
func moveAside(src, dst string) error {
	if _, err := os.Lstat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.Rename(src, dst)
}

// restore moves a backup back to target during rollback, returning any error so
// callers can surface rollback failures rather than silently losing data.
func restore(backup, target string) error {
	if _, err := os.Lstat(backup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return os.Rename(backup, target)
}

// validateSource validates the repository and ref, rejecting "."/".." ref
// segments so the constructed tarball URL cannot escape the /tarball/<ref> path.
func validateSource(repo, ref string) error {
	if !repoRe.MatchString(repo) {
		return fmt.Errorf("update: invalid manifests repository: %q", repo)
	}
	if !refRe.MatchString(ref) {
		return fmt.Errorf("update: invalid manifests ref: %q", ref)
	}
	for _, seg := range strings.Split(ref, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("update: invalid manifests ref: %q", ref)
		}
	}
	return nil
}
