package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/buildinfo"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Prints the tt-env version, the git commit it was built from, and the build date.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
		return nil
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
	RootCmd.Version = buildinfo.Version
	RootCmd.SetVersionTemplate(buildinfo.String() + "\n")
}
