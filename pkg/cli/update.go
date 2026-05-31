package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/update"
)

// runUpdate refreshes the local manifest cache from the configured GitHub
// source. The proto1 "--self" binary self-update is not supported in the Go
// build, which is distributed via package managers or "go install".
func runUpdate(cmd *cobra.Command, self bool) error {
	if self {
		return errors.New("binary self-update (--self) is not supported in the Go build; update tt-env via your package manager or \"go install\"")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	token, err := resolveGitHubToken(ctx)
	if err != nil {
		return err
	}

	u := &update.Updater{
		Root:    ttHome(),
		Repo:    os.Getenv("TT_UPDATE_MANIFESTS_REPO"),
		Ref:     os.Getenv("TT_UPDATE_MANIFESTS_REF"),
		Token:   token,
		Fetcher: update.CurlFetcher{},
	}
	res, err := u.Update(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Updated %d release manifest(s) from %s@%s.\n",
		res.ReleaseCount, res.Repo, res.Ref)
	return nil
}

// resolveGitHubToken returns a GitHub token from GITHUB_TOKEN, GH_TOKEN, or
// "gh auth token", mirroring proto1. The token is never logged.
func resolveGitHubToken(ctx context.Context) (string, error) {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t, nil
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, nil
	}
	if out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output(); err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t, nil
		}
	}
	return "", errors.New("authentication required: set GITHUB_TOKEN or run: gh auth login")
}
