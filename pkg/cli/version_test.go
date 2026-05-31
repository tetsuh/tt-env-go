package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/buildinfo"
)

// resetRootFlags clears the cobra-managed help and version flags so a prior
// test that triggered --help cannot suppress version output here.
func resetRootFlags() {
	for _, name := range []string{"help", "version"} {
		if f := RootCmd.Flags().Lookup(name); f != nil {
			_ = f.Value.Set("false")
		}
	}
}

func TestVersionCommandOutput(t *testing.T) {
	origOut := RootCmd.OutOrStdout()
	origErr := RootCmd.ErrOrStderr()
	t.Cleanup(func() {
		RootCmd.SetOut(origOut)
		RootCmd.SetErr(origErr)
		RootCmd.SetArgs(nil)
	})

	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"version"})
	resetRootFlags()

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("expected version command to succeed, got %v", err)
	}

	got := strings.TrimSpace(buf.String())
	if got != buildinfo.String() {
		t.Errorf("version output = %q, want %q", got, buildinfo.String())
	}
}

func TestVersionFlagOutput(t *testing.T) {
	origOut := RootCmd.OutOrStdout()
	origErr := RootCmd.ErrOrStderr()
	t.Cleanup(func() {
		RootCmd.SetOut(origOut)
		RootCmd.SetErr(origErr)
		RootCmd.SetArgs(nil)
	})

	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"--version"})
	resetRootFlags()

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("expected --version to succeed, got %v", err)
	}

	got := strings.TrimSpace(buf.String())
	if got != buildinfo.String() {
		t.Errorf("--version output = %q, want %q", got, buildinfo.String())
	}
}
