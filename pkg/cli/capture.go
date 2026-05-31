package cli

import (
	"log/slog"

	"github.com/spf13/cobra"
)

var (
	captureDryRun bool
	captureForce  bool
	captureBase   string
)

// captureCmd represents the capture command
var captureCmd = &cobra.Command{
	Use:   "capture <release>",
	Short: "Capture a local-only stack release manifest",
	Long:  `Probes the local system and captures a local-only Tenstorrent stack release manifest for the given release.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Running capture command",
			slog.String("release", args[0]),
			slog.Bool("dry-run", captureDryRun),
			slog.Bool("force", captureForce),
			slog.String("base", captureBase),
		)
	},
}

func init() {
	captureCmd.Flags().BoolVar(&captureDryRun, "dry-run", false, "print the captured manifest without writing it")
	captureCmd.Flags().BoolVar(&captureForce, "force", false, "overwrite an existing local manifest")
	captureCmd.Flags().StringVar(&captureBase, "base", "", "use an existing release manifest as the capture template")
	RootCmd.AddCommand(captureCmd)
}
