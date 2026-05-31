package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/manifest"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// catalogEntry is a release advertised by a manifest under ${TT_HOME}/releases,
// together with whether it is installed locally.
type catalogEntry struct {
	Release   string
	Installed bool
}

// runUse switches the active release by updating the current symlink.
func runUse(cmd *cobra.Command, release string) error {
	inst := &version.Installer{Root: ttHome()}
	if err := inst.Use(release); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Now using release %s.\n", release)
	return nil
}

// runList prints the release catalog, marking each release installed or
// available, mirroring proto1 list_releases.
func runList(cmd *cobra.Command) error {
	root := ttHome()
	inst := &version.Installer{Root: root}
	entries, warnings := collectCatalog(filepath.Join(root, "releases"), inst)

	for _, w := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping invalid release manifest: %s\n", w)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Releases")
	if len(entries) == 0 {
		fmt.Fprintln(out, "  (none)")
		return nil
	}
	for _, e := range entries {
		state := "available"
		if e.Installed {
			state = "installed"
		}
		fmt.Fprintf(out, "  %s [%s]\n", e.Release, state)
	}
	return nil
}

// collectCatalog reads every *.json manifest under releasesDir, returning the
// advertised releases sorted by name and the paths of any manifests that could
// not be parsed. A missing releasesDir yields an empty catalog.
func collectCatalog(releasesDir string, inst *version.Installer) ([]catalogEntry, []string) {
	dir, err := os.Open(releasesDir)
	if err != nil {
		return nil, nil
	}
	defer dir.Close()

	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, nil
	}

	var entries []catalogEntry
	var warnings []string
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(releasesDir, name)
		m, err := manifest.Load(path)
		if err != nil {
			warnings = append(warnings, path)
			continue
		}
		entries = append(entries, catalogEntry{
			Release:   m.Release,
			Installed: inst.IsInstalled(m.Release),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Release < entries[j].Release
	})
	return entries, warnings
}
