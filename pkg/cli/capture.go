package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/capture"
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
	RunE:  runCapture,
}

// runCapture probes the installed base release and writes (or, with --dry-run,
// prints) a local-only manifest for the requested release.
func runCapture(cmd *cobra.Command, args []string) error {
	release := args[0]

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	cap := &capture.Capturer{
		Root: ttHome(),
		Logf: func(format string, a ...any) {
			fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", a...)
		},
	}

	res, err := cap.Capture(ctx, release, capture.Options{
		Base:   captureBase,
		DryRun: captureDryRun,
		Force:  captureForce,
	})
	if err != nil {
		return err
	}

	if captureDryRun {
		if _, err := cmd.OutOrStdout().Write(res.ManifestJSON); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	captureCmd.Flags().BoolVar(&captureDryRun, "dry-run", false, "print the captured manifest without writing it")
	captureCmd.Flags().BoolVar(&captureForce, "force", false, "overwrite an existing local manifest")
	captureCmd.Flags().StringVar(&captureBase, "base", "", "use an existing release manifest as the capture template")
	RootCmd.AddCommand(captureCmd)
}
