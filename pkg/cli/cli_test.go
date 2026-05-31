package cli

import (
	"bytes"
	"testing"
)

func TestCommandsAdded(t *testing.T) {
	commands := []string{"install", "remove", "use", "list", "status", "update", "diff"}

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
