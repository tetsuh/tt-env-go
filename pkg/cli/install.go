package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/install"
)

var (
	dryRun        bool
	force         bool
	installLatest bool
	installBase   string
)

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:   "install <release>",
	Short: "Install a specific Tenstorrent stack release",
	Long: `Downloads, processes, and installs dependencies for a Tenstorrent stack release.

With --latest, installs the latest available versions (unpinned system and
Python packages, git components at their remote HEAD) instead of the pinned
versions in the manifest. Use --base to supply the release structure to follow
when authoring a new release on hardware; the installed versions can then be
recorded with "tt-env capture".`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

// runInstall installs the requested release via the install orchestrator.
func runInstall(cmd *cobra.Command, args []string) error {
	release := args[0]

	if installBase != "" && !installLatest {
		return fmt.Errorf("--base is only valid together with --latest")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	out := cmd.OutOrStdout()
	orch := &install.Orchestrator{
		Root: ttHome(),
		Logf: func(format string, a ...any) {
			fmt.Fprintf(out, format+"\n", a...)
		},
	}

	_, err := orch.Install(ctx, release, install.Options{
		DryRun: dryRun,
		Force:  force,
		Latest: installLatest,
		Base:   installBase,
	})
	return err
}

func init() {
	installCmd.Flags().BoolVar(&dryRun, "dry-run", false, "perform a dry-run without installing")
	installCmd.Flags().BoolVar(&force, "force", false, "force reinstall if release is already installed")
	installCmd.Flags().BoolVar(&installLatest, "latest", false, "install the latest available versions instead of the pinned manifest versions")
	installCmd.Flags().StringVar(&installBase, "base", "", "release whose structure seeds a --latest install (defaults to <release>)")
	RootCmd.AddCommand(installCmd)
}
