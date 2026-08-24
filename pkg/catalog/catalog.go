// Package catalog resolves release manifests between the two manifest
// locations under TT_HOME:
//
//	releases/        the fetched catalog cache, replaced wholesale by
//	                 "tt-env update"
//	releases.local/  user-authored local manifests (e.g. "tt-env capture"),
//	                 never modified by "tt-env update"
//
// A release defined in both locations resolves to the local manifest: local
// overrides catalog.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
)

// LocalDirName is the directory under TT_HOME that stores user-authored,
// local-only release manifests. It is deliberately a sibling of releases/ so
// the wholesale catalog swap performed by update cannot delete local
// manifests.
const LocalDirName = "releases.local"

// CatalogDir returns the fetched catalog cache directory under root.
func CatalogDir(root string) string {
	return filepath.Join(root, "releases")
}

// LocalDir returns the local-only manifest directory under root.
func LocalDir(root string) string {
	return filepath.Join(root, LocalDirName)
}

// Entry is a release advertised by a manifest in one of the two locations.
type Entry struct {
	Release string
	// Local is true when the winning manifest was found under releases.local/.
	Local bool
}

// Path resolves the manifest path for release, preferring a local manifest
// over a catalog manifest. The boolean reports whether the resolved path is a
// local manifest. It returns an error when release is not a plain file name
// or no manifest exists in either location.
func Path(root, release string) (string, bool, error) {
	if err := validateName(release); err != nil {
		return "", false, err
	}
	local := filepath.Join(LocalDir(root), release+".json")
	if _, err := os.Stat(local); err == nil {
		return local, true, nil
	}
	cat := filepath.Join(CatalogDir(root), release+".json")
	if _, err := os.Stat(cat); err == nil {
		return cat, false, nil
	}
	return "", false, fmt.Errorf("catalog: no release manifest for %q in %s or %s", release, CatalogDir(root), LocalDir(root))
}

// Available lists the releases advertised in both locations, sorted by name.
// When both locations define the same release, the local entry wins. The
// second return value lists manifest paths that could not be parsed; missing
// directories yield an empty catalog.
func Available(root string) ([]Entry, []string) {
	releases := make(map[string]bool) // release -> local
	var warnings []string
	collect := func(dir string, local bool) {
		names, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range names {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			m, err := manifest.Load(path)
			if err != nil {
				warnings = append(warnings, path)
				continue
			}
			releases[m.Release] = local || releases[m.Release]
		}
	}
	collect(CatalogDir(root), false)
	collect(LocalDir(root), true)

	entries := make([]Entry, 0, len(releases))
	for release, local := range releases {
		entries = append(entries, Entry{Release: release, Local: local})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Release < entries[j].Release
	})
	return entries, warnings
}

// validateName rejects release names that could escape the manifest
// directories. Callers that already validate release names (version.
// ValidateRelease) pass by construction; this guard keeps Path safe to call
// with untrusted input.
func validateName(release string) error {
	if release == "" {
		return fmt.Errorf("catalog: release name must not be empty")
	}
	if release == "." || release == ".." || strings.ContainsAny(release, `/\`) {
		return fmt.Errorf("catalog: invalid release name %q", release)
	}
	return nil
}
