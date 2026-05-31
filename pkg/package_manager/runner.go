package packagemanager

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner abstracts execution of external commands so that adapters can
// be tested without running real package-manager commands. Run returns the
// combined stdout/stderr output and any execution error.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner is the production CommandRunner. It executes commands with
// os/exec and returns their combined output.
type ExecRunner struct{}

// Run executes name with args and returns the combined stdout/stderr output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// RecordedCommand is a single command captured by MockRunner.
type RecordedCommand struct {
	Name string
	Args []string
}

// String renders the command as a shell-like "name arg1 arg2" string.
func (c RecordedCommand) String() string {
	if len(c.Args) == 0 {
		return c.Name
	}
	return c.Name + " " + strings.Join(c.Args, " ")
}

// CommandResponse is a programmed result for a MockRunner invocation.
type CommandResponse struct {
	Output []byte
	Err    error
}

// MockRunner is a CommandRunner that records every invocation and returns
// programmed results, letting tests assert command sequences. Responses are
// consumed first-in-first-out. When they are exhausted, Run returns nil output
// and a nil error, unless Strict is set, in which case Run returns an error so
// that unexpected commands fail the test loudly. If RunFunc is set it overrides
// Responses entirely.
type MockRunner struct {
	Commands  []RecordedCommand
	Responses []CommandResponse
	RunFunc   func(ctx context.Context, name string, args ...string) ([]byte, error)
	Strict    bool
}

// Run records the command and returns the next programmed response.
func (m *MockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.Commands = append(m.Commands, RecordedCommand{
		Name: name,
		Args: append([]string(nil), args...),
	})

	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args...)
	}

	if len(m.Responses) > 0 {
		resp := m.Responses[0]
		m.Responses = m.Responses[1:]
		return resp.Output, resp.Err
	}
	if m.Strict {
		return nil, fmt.Errorf("unexpected command with no programmed response: %s", RecordedCommand{Name: name, Args: args})
	}
	return nil, nil
}

// CommandStrings returns the recorded commands as shell-like strings, which is
// convenient for asserting command sequences in tests.
func (m *MockRunner) CommandStrings() []string {
	out := make([]string, len(m.Commands))
	for i, c := range m.Commands {
		out[i] = c.String()
	}
	return out
}
