package packagemanager

import (
	"context"
	"fmt"
	"strings"
)

// DnfManager is a PackageManager adapter for Fedora/RHEL hosts. It translates
// the PackageManager operations into dnf / rpm / dnf config-manager commands run
// through an injected CommandRunner.
type DnfManager struct {
	// Runner executes the underlying commands. If nil, ExecRunner is used.
	Runner CommandRunner
	// Sudo controls whether mutating commands (update, add-repo, install,
	// remove) are prefixed with sudo. Read-only queries never use sudo.
	Sudo bool
}

var _ PackageManager = (*DnfManager)(nil)

// NewDnfManager returns a DnfManager that runs commands through runner with sudo
// enabled. If runner is nil the production ExecRunner is used.
func NewDnfManager(runner CommandRunner) *DnfManager {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &DnfManager{Runner: runner, Sudo: true}
}

// runner returns the configured CommandRunner, defaulting to ExecRunner.
func (m *DnfManager) runner() CommandRunner {
	if m.Runner == nil {
		return ExecRunner{}
	}
	return m.Runner
}

// runPrivileged runs a mutating command, prefixing sudo when enabled, and wraps
// any failure with the command and its combined output.
func (m *DnfManager) runPrivileged(ctx context.Context, name string, args ...string) error {
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

// Update refreshes the local dnf package metadata.
func (m *DnfManager) Update(ctx context.Context) error {
	return m.runPrivileged(ctx, "dnf", "makecache")
}

// AddRepo configures a dnf repository using dnf config-manager.
func (m *DnfManager) AddRepo(ctx context.Context, repo Repository) error {
	if err := validateArg("repository URI", repo.URI); err != nil {
		return err
	}
	return m.runPrivileged(ctx, "dnf", "config-manager", "--add-repo", repo.URI)
}

// Install installs the given packages, honoring version pins as name-version.
func (m *DnfManager) Install(ctx context.Context, pkgs ...Package) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("dnf install: no packages given")
	}
	specs := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if err := validateArg("package name", pkg.Name); err != nil {
			return err
		}
		spec := pkg.Name
		if pkg.Version != "" {
			spec = pkg.Name + "-" + pkg.Version
		}
		specs = append(specs, spec)
	}
	args := append([]string{"install", "-y", "--"}, specs...)
	return m.runPrivileged(ctx, "dnf", args...)
}

// Remove removes the named packages.
func (m *DnfManager) Remove(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return fmt.Errorf("dnf remove: no packages given")
	}
	for _, name := range names {
		if err := validateArg("package name", name); err != nil {
			return err
		}
	}
	args := append([]string{"remove", "-y", "--"}, names...)
	return m.runPrivileged(ctx, "dnf", args...)
}

// IsInstalled reports whether name is installed, using rpm. It returns false for
// packages that are not installed, and an error for any other rpm failure.
func (m *DnfManager) IsInstalled(ctx context.Context, name string) (bool, error) {
	if err := validateArg("package name", name); err != nil {
		return false, err
	}
	out, err := m.runner().Run(ctx, "rpm", "-q", name)
	if err == nil {
		return true, nil
	}
	// rpm -q exits non-zero and prints "package <name> is not installed" when
	// the package is absent; treat that as not installed but surface any other
	// failure (e.g. missing binary).
	if strings.Contains(string(out), "is not installed") {
		return false, nil
	}
	return false, commandError("rpm", []string{"-q", name}, out, err)
}
