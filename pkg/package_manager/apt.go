package packagemanager

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// AptManager is a PackageManager adapter for Debian/Ubuntu hosts. It translates
// the PackageManager operations into apt-get / dpkg / add-apt-repository
// commands run through an injected CommandRunner.
type AptManager struct {
	// Runner executes the underlying commands. If nil, ExecRunner is used.
	Runner CommandRunner
	// Sudo controls whether mutating commands (update, add-repo, install,
	// remove) are prefixed with sudo. Read-only queries never use sudo.
	Sudo bool
}

var _ PackageManager = (*AptManager)(nil)

// NewAptManager returns an AptManager that runs commands through runner with
// sudo enabled. If runner is nil the production ExecRunner is used.
func NewAptManager(runner CommandRunner) *AptManager {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &AptManager{Runner: runner, Sudo: true}
}

// runner returns the configured CommandRunner, defaulting to ExecRunner.
func (m *AptManager) runner() CommandRunner {
	if m.Runner == nil {
		return ExecRunner{}
	}
	return m.Runner
}

// runPrivileged runs a mutating command, prefixing sudo when enabled, and wraps
// any failure with the command and its combined output.
func (m *AptManager) runPrivileged(ctx context.Context, name string, args ...string) error {
	if m.Sudo {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	out, err := m.runner().Run(ctx, name, args...)
	if err != nil {
		return commandError(name, args, out, err)
	}
	return nil
}

// Update refreshes the local apt package metadata.
func (m *AptManager) Update(ctx context.Context) error {
	return m.runPrivileged(ctx, "apt-get", "update")
}

// AddRepo configures an apt repository using add-apt-repository.
func (m *AptManager) AddRepo(ctx context.Context, repo Repository) error {
	if err := validateArg("repository URI", repo.URI); err != nil {
		return err
	}
	return m.runPrivileged(ctx, "add-apt-repository", "-y", repo.URI)
}

// Install installs the given packages, honoring version pins as name=version.
func (m *AptManager) Install(ctx context.Context, pkgs ...Package) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("apt install: no packages given")
	}
	specs := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if err := validateArg("package name", pkg.Name); err != nil {
			return err
		}
		spec := pkg.Name
		if pkg.Version != "" {
			spec = pkg.Name + "=" + pkg.Version
		}
		specs = append(specs, spec)
	}
	args := append([]string{"install", "-y", "--"}, specs...)
	return m.runPrivileged(ctx, "apt-get", args...)
}

// Remove removes the named packages.
func (m *AptManager) Remove(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return fmt.Errorf("apt remove: no packages given")
	}
	for _, name := range names {
		if err := validateArg("package name", name); err != nil {
			return err
		}
	}
	args := append([]string{"remove", "-y", "--"}, names...)
	return m.runPrivileged(ctx, "apt-get", args...)
}

// IsInstalled reports whether name is installed, using dpkg-query. It returns
// false for packages that are not installed or unknown, and an error for any
// other dpkg-query failure.
func (m *AptManager) IsInstalled(ctx context.Context, name string) (bool, error) {
	if err := validateArg("package name", name); err != nil {
		return false, err
	}
	out, err := m.runner().Run(ctx, "dpkg-query", "-W", "-f=${Status}", name)
	status := strings.TrimSpace(string(out))
	if strings.HasPrefix(status, "install ok installed") {
		return true, nil
	}
	if err == nil {
		return false, nil
	}
	// dpkg-query exits with status 1 for unknown/not-installed packages.
	// Preferring the exit code keeps the check correct regardless of locale; the
	// message match is only a fallback for runners that do not surface an exit
	// code. Any other failure (e.g. missing binary) is surfaced as an error.
	if isExitCode(err, 1) {
		return false, nil
	}
	if strings.Contains(string(out), "no packages found matching") {
		return false, nil
	}
	return false, commandError("dpkg-query", []string{"-W", "-f=${Status}", name}, out, err)
}

// validateArg rejects empty values and values that look like command-line
// options, which apt and add-apt-repository would otherwise misinterpret.
func validateArg(label, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", label)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s must not start with '-': %q", label, value)
	}
	return nil
}

// isExitCode reports whether err is an *exec.ExitError whose process exited with
// the given status code. It lets adapters classify "not installed" by exit code
// rather than locale-sensitive output.
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == code
	}
	return false
}

// commandError wraps a command failure with its name, args, and trimmed output.
func commandError(name string, args []string, out []byte, err error) error {
	cmd := name
	if len(args) > 0 {
		cmd += " " + strings.Join(args, " ")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return fmt.Errorf("%s: %w: %s", cmd, err, trimmed)
}
