package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandsAdded(t *testing.T) {
	commands := []string{"install", "remove", "use", "list", "status", "update", "diff", "capture"}

	for _, cmdName := range commands {
		found := false
		for _, cmd := range RootCmd.Commands() {
			if cmd.Name() == cmdName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected command %s to be registered under RootCmd", cmdName)
		}
	}
}

func TestExecuteRootCommand(t *testing.T) {
	// Save original values
	origOut := RootCmd.OutOrStdout()
	origErr := RootCmd.ErrOrStderr()
	origArgs := RootCmd.Args

	// Restore after test
	t.Cleanup(func() {
		RootCmd.SetOut(origOut)
		RootCmd.SetErr(origErr)
		RootCmd.SetArgs(nil)
		if origArgs != nil {
			RootCmd.Args = origArgs
		}
	})

	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs([]string{"--help"})

	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("expected execute to succeed, got %v", err)
	}

	if buf.Len() == 0 {
		t.Errorf("expected output to be written, got empty buffer")
	}
}

// findCommand returns the registered subcommand with the given name.
func findCommand(name string) *cobra.Command {
	for _, cmd := range RootCmd.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

func TestDiffRequiresTwoReleases(t *testing.T) {
	diff := findCommand("diff")
	if diff == nil {
		t.Fatal("diff command is not registered")
	}

	if err := diff.Args(diff, []string{"a"}); err == nil {
		t.Error("expected diff to reject a single argument")
	}
	if err := diff.Args(diff, []string{"a", "b"}); err != nil {
		t.Errorf("expected diff to accept two arguments, got %v", err)
	}
	if err := diff.Args(diff, []string{"a", "b", "c"}); err == nil {
		t.Error("expected diff to reject three arguments")
	}
}

func TestCaptureRequiresSingleRelease(t *testing.T) {
	capture := findCommand("capture")
	if capture == nil {
		t.Fatal("capture command is not registered")
	}

	if err := capture.Args(capture, []string{}); err == nil {
		t.Error("expected capture to reject zero arguments")
	}
	if err := capture.Args(capture, []string{"2026.05.16"}); err != nil {
		t.Errorf("expected capture to accept one argument, got %v", err)
	}
	if err := capture.Args(capture, []string{"a", "b"}); err == nil {
		t.Error("expected capture to reject two arguments")
	}
}
