package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/logger"
)

var (
	logLevel string
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "tt-env",
	Short: "tt-env is a tool for managing Tenstorrent stack installations",
	Long:  `A statically compiled, production-ready Go implementation of the Tenstorrent tt-env environment manager.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.Init(logLevel)
	},
	SilenceErrors: true,
	SilenceUsage:  true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the RootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "info", "set log level (debug, info, warn, error)")
}
