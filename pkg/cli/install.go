package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/install"
)

var (
	dryRun bool
	force  bool
)

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:   "install <release>",
	Short: "Install a specific Tenstorrent stack release",
	Long:  `Downloads, processes, and installs dependencies for a Tenstorrent stack release.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

// runInstall installs the requested release via the install orchestrator.
func runInstall(cmd *cobra.Command, args []string) error {
	release := args[0]

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

	_, err := orch.Install(ctx, release, dryRun, force)
	return err
}

func init() {
	installCmd.Flags().BoolVar(&dryRun, "dry-run", false, "perform a dry-run without installing")
	installCmd.Flags().BoolVar(&force, "force", false, "force reinstall if release is already installed")
	RootCmd.AddCommand(installCmd)
}
