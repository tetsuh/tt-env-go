package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/kmd"
	"github.com/tetsuh/tt-env-go/pkg/status"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// ttHome resolves the TT_HOME root, mirroring the shim runtime: the TT_HOME
// environment variable if set, otherwise ${HOME}/.tt-env.
func ttHome() string {
	if home := os.Getenv("TT_HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".tt-env")
	}
	return ".tt-env"
}

// runStatus aggregates the environment probes and renders the summary.
func runStatus(cmd *cobra.Command) error {
	root := ttHome()
	reporter := &status.Reporter{
		Detector:   &status.Detector{},
		Installer:  &version.Installer{Root: root},
		SecureBoot: &kmd.SecureBootChecker{},
		KMDVersion: &kmd.VersionProber{},
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	summary := reporter.Report(ctx)
	return summary.Render(cmd.OutOrStdout())
}
