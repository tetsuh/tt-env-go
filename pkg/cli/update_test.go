package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUpdateCommandRejectsSelf(t *testing.T) {
	buf := new(bytes.Buffer)
	updateCmd.SetOut(buf)
	t.Cleanup(func() { updateCmd.SetOut(nil) })

	err := runUpdate(updateCmd, true)
	if err == nil {
		t.Fatal("expected error for --self")
	}
	if !strings.Contains(err.Error(), "self-update") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateCommandHasSelfFlag(t *testing.T) {
	if updateCmd.Flags().Lookup("self") == nil {
		t.Error("update command must declare a --self flag")
	}
	if updateCmd.Args == nil {
		t.Fatal("update command must declare an Args validator")
	}
	if err := updateCmd.Args(updateCmd, []string{"extra"}); err == nil {
		t.Error("expected update to reject positional arguments")
	}
}

func TestResolveGitHubTokenPrefersEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	t.Setenv("GH_TOKEN", "from-gh-token")

	got, err := resolveGitHubToken(context.Background())
	if err != nil {
		t.Fatalf("resolveGitHubToken() error = %v", err)
	}
	if got != "from-github-token" {
		t.Errorf("token = %q, want from-github-token", got)
	}
}

func TestResolveGitHubTokenFallsBackToGHToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "from-gh-token")

	got, err := resolveGitHubToken(context.Background())
	if err != nil {
		t.Fatalf("resolveGitHubToken() error = %v", err)
	}
	if got != "from-gh-token" {
		t.Errorf("token = %q, want from-gh-token", got)
	}
}
