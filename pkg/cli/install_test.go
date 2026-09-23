package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func installFlagCommand(t *testing.T, flags map[string]string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("upgrade", false, "")
	cmd.Flags().Bool("latest", false, "")
	cmd.Flags().String("like", "", "")
	cmd.Flags().String("base", "", "")
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	var warnings bytes.Buffer
	cmd.SetErr(&warnings)
	return cmd, &warnings
}

func TestInstallUpgradeFlagsAndAliases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		flags      map[string]string
		wantLike   string
		wantWarn   string
		wantErr    string
		wantUpgrad bool
	}{
		{name: "upgrade", flags: map[string]string{"upgrade": "true", "like": "0.75.0"}, wantLike: "0.75.0", wantUpgrad: true},
		{name: "deprecated aliases", flags: map[string]string{"latest": "true", "base": "0.75.0"}, wantLike: "0.75.0", wantWarn: "--latest is an alias for --upgrade", wantUpgrad: true},
		{name: "same template aliases", flags: map[string]string{"upgrade": "true", "like": "0.75.0", "base": "0.75.0"}, wantLike: "0.75.0", wantWarn: "--base is an alias for --like", wantUpgrad: true},
		{name: "conflicting templates", flags: map[string]string{"upgrade": "true", "like": "0.75.0", "base": "0.74.0"}, wantErr: "different template releases"},
		{name: "template without upgrade", flags: map[string]string{"like": "0.75.0"}, wantErr: "--like requires --upgrade"},
		{name: "legacy base without upgrade", flags: map[string]string{"base": "0.75.0"}, wantErr: "--like requires --upgrade"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, warnings := installFlagCommand(t, tc.flags)
			got, err := installFlags(cmd)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("installFlags() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Upgrade != tc.wantUpgrad || got.Like != tc.wantLike {
				t.Errorf("options = %+v, want upgrade=%t like=%q", got, tc.wantUpgrad, tc.wantLike)
			}
			if tc.wantWarn != "" && !strings.Contains(warnings.String(), tc.wantWarn) {
				t.Errorf("warning = %q, want %q", warnings.String(), tc.wantWarn)
			}
			if tc.wantWarn == "" && warnings.Len() != 0 {
				t.Errorf("unexpected warning: %q", warnings.String())
			}
		})
	}
}

func TestInstallDeprecatedFlagsHiddenFromHelp(t *testing.T) {
	for _, name := range []string{"latest", "base"} {
		if flag := installCmd.Flags().Lookup(name); flag == nil || !flag.Hidden {
			t.Errorf("%s should be registered but hidden", name)
		}
	}
	for _, name := range []string{"upgrade", "like"} {
		if flag := installCmd.Flags().Lookup(name); flag == nil || flag.Hidden {
			t.Errorf("%s should be visible", name)
		}
	}
}
