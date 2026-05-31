package cli

import (
	"log/slog"

	"github.com/spf13/cobra"
)

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:   "remove <release>",
	Short: "Remove a specific Tenstorrent stack release",
	Long:  `Uninstalls all local components and shims for a specific release.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Running remove command", slog.String("release", args[0]))
	},
}

// useCmd represents the use command
var useCmd = &cobra.Command{
	Use:   "use <release>",
	Short: "Switch the active Tenstorrent stack release",
	Long:  `Updates the active version symlink and configures system shims for the specified release.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Running use command", slog.String("release", args[0]))
	},
}

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed and available releases",
	Long:  `Displays a list of all local installed releases and all available remote releases in the catalog.`,
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Running list command")
	},
}

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show environment and hardware status",
	Long:  `Probes and prints the active Tenstorrent environment version, installed releases, detected hardware, and KMD/Secure Boot state.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(cmd)
	},
}

// updateCmd represents the update command
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update release manifest catalogs",
	Long:  `Fetches and updates the local cache of Tenstorrent stack release manifests from the remote repository.`,
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Running update command")
	},
}

// diffCmd represents the diff command
var diffCmd = &cobra.Command{
	Use:   "diff <release-a> <release-b>",
	Short: "Compare two release manifests",
	Long:  `Displays differences in versions and dependencies between two Tenstorrent release manifests.`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		slog.Info("Running diff command", slog.String("release-a", args[0]), slog.String("release-b", args[1]))
	},
}

func init() {
	RootCmd.AddCommand(removeCmd)
	RootCmd.AddCommand(useCmd)
	RootCmd.AddCommand(listCmd)
	RootCmd.AddCommand(statusCmd)
	RootCmd.AddCommand(updateCmd)
	RootCmd.AddCommand(diffCmd)
}
