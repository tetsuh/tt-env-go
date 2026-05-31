package cli

import (
	"log/slog"

	"github.com/spf13/cobra"
)

var (
	dryRun bool
	force  bool
)

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:   "install [release]",
	Short: "Install a specific Tenstorrent stack release",
	Long:  `Downloads, processes, and installs dependencies for a Tenstorrent stack release.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		release := "latest"
		if len(args) > 0 {
			release = args[0]
		}
		slog.Info("Running install command",
			slog.String("release", release),
			slog.Bool("dry-run", dryRun),
			slog.Bool("force", force),
		)
	},
}

func init() {
	installCmd.Flags().BoolVar(&dryRun, "dry-run", false, "perform a dry-run without installing")
	installCmd.Flags().BoolVar(&force, "force", false, "force reinstall if release is already installed")
	RootCmd.AddCommand(installCmd)
}
