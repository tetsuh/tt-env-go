package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/catalog"
	"github.com/tetsuh/tt-env-go/pkg/manifest"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// runUse switches the active release by updating the current symlink.
func runUse(cmd *cobra.Command, release string) error {
	inst := &version.Installer{Root: ttHome()}
	if err := inst.Use(release); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Now using release %s.\n", release)
	return nil
}

// runRemove uninstalls a release and clears the active symlink when needed.
func runRemove(cmd *cobra.Command, release string) error {
	inst := &version.Installer{Root: ttHome()}
	if err := inst.Remove(release); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed release %s.\n", release)
	return nil
}

// runDiff loads two release manifests and prints their side-by-side
// differences, mirroring proto1 diff_releases.
func runDiff(cmd *cobra.Command, leftRelease, rightRelease string) error {
	left, err := loadReleaseManifest(leftRelease)
	if err != nil {
		return err
	}
	right, err := loadReleaseManifest(rightRelease)
	if err != nil {
		return err
	}
	return manifest.Diff(left, right).Render(cmd.OutOrStdout())
}

// loadReleaseManifest resolves and loads the manifest for release from the
// local manifest directory and the catalog cache (local overrides catalog),
// validating the release name first.
func loadReleaseManifest(release string) (*manifest.Manifest, error) {
	if err := version.ValidateRelease(release); err != nil {
		return nil, err
	}
	path, _, err := catalog.Path(ttHome(), release)
	if err != nil {
		return nil, err
	}
	return manifest.Load(path)
}

// runList prints the release catalog, marking each release installed,
// available, and/or local-only, mirroring proto1 list_releases.
func runList(cmd *cobra.Command) error {
	root := ttHome()
	inst := &version.Installer{Root: root}
	entries, warnings := catalog.Available(root)

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
		if inst.IsInstalled(e.Release) {
			state = "installed"
		}
		if e.Local {
			state += ", local"
		}
		fmt.Fprintf(out, "  %s [%s]\n", e.Release, state)
	}
	return nil
}
