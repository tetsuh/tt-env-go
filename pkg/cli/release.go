package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/catalog"
	"github.com/tetsuh/tt-env-go/pkg/lock"
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
// differences, mirroring proto1 diff_releases. An installed release resolves
// to its lock (versions/<release>/manifest.json), so a diff can compare the
// actually installed versions against catalog intent.
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

// loadReleaseManifest resolves the manifest for release. An installed release
// prefers its lock (the resolved versions that were installed); otherwise the
// local manifest directory and the catalog cache are searched (local overrides
// catalog).
func loadReleaseManifest(release string) (*manifest.Manifest, error) {
	if err := version.ValidateRelease(release); err != nil {
		return nil, err
	}
	root := ttHome()
	inst := &version.Installer{Root: root}
	if inst.IsInstalled(release) {
		l, err := lock.Read(inst.ReleaseDir(release))
		if err != nil && !errors.Is(err, lock.ErrNotLocked) {
			return nil, err
		}
		if err == nil {
			return &l.Manifest, nil
		}
		// Installed before locks existed: fall through to the manifest catalog.
	}
	path, _, err := catalog.Path(root, release)
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
		line := fmt.Sprintf("  %s", e.Release)
		if inst.IsInstalled(e.Release) {
			state = "installed"
			// Show the install provenance recorded in the lock, when present.
			if l, err := lock.Read(inst.ReleaseDir(e.Release)); err == nil {
				line += fmt.Sprintf(" (%s)", l.Describe())
			} else if !errors.Is(err, lock.ErrNotLocked) {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: release %s has an unreadable lock: %v\n", e.Release, err)
			}
		}
		if e.Local {
			state += ", local"
		}
		fmt.Fprintf(out, "%s [%s]\n", line, state)
	}
	return nil
}
