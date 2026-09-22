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

// resolved is the winning manifest for one release.
type resolved struct {
	path  string
	local bool
}

// Path resolves the manifest that declares release, preferring a local
// manifest over a catalog manifest. The boolean reports whether the resolved
// manifest is local. Manifests are matched by their declared "release" field
// — the same rule Available and list use — and a manifest that fails to load
// is never selected, so an invalid local manifest cannot shadow a valid
// catalog manifest. It returns an error when release is not a plain file name
// or no manifest declares the release in either location.
func Path(root, release string) (string, bool, error) {
	if err := validateName(release); err != nil {
		return "", false, err
	}
	winners, _ := scan(root)
	if r, ok := winners[release]; ok {
		return r.path, r.local, nil
	}
	return "", false, fmt.Errorf("catalog: no release manifest for %q in %s or %s", release, CatalogDir(root), LocalDir(root))
}

// Available lists the releases advertised in both locations, sorted by name.
// When both locations declare the same release, the local entry wins; a
// manifest that fails to load is skipped in either location. The second
// return value lists manifest paths that could not be parsed; missing
// directories yield an empty catalog.
func Available(root string) ([]Entry, []string) {
	winners, warnings := scan(root)
	entries := make([]Entry, 0, len(winners))
	for release, r := range winners {
		entries = append(entries, Entry{Release: release, Local: r.local})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Release < entries[j].Release
	})
	return entries, warnings
}

// scan reads both manifest locations and returns the winning manifest per
// declared release: local manifests override catalog manifests, and manifests
// that fail to load are skipped (reported as warnings) regardless of location.
func scan(root string) (map[string]resolved, []string) {
	winners := make(map[string]resolved)
	var warnings []string
	collect := func(dir string, local bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			m, err := manifest.Load(path)
			if err != nil {
				warnings = append(warnings, path)
				continue
			}
			if winner, ok := winners[m.Release]; !ok || local && !winner.local {
				winners[m.Release] = resolved{path: path, local: local}
			}
		}
	}
	collect(CatalogDir(root), false)
	collect(LocalDir(root), true)
	return winners, warnings
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
