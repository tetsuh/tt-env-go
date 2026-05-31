// Package venv provisions per-release Python virtualenvs and installs the
// pinned packages declared in a release manifest's python_packages map.
//
// Provisioning runs through the package-manager CommandRunner abstraction so it
// can be unit tested without creating real virtualenvs. A virtualenv is created
// with "python -m venv" and packages are installed with the venv's own
// interpreter via "python -m pip install".
package venv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// DefaultSubdir is the directory name, relative to a release directory, under
// which a release's virtualenv is created.
const DefaultSubdir = "venv"

// DefaultPython is the interpreter used to create virtualenvs when Provisioner
// does not override it.
const DefaultPython = "python3"

// Provisioner creates virtualenvs and installs pinned Python packages into them.
type Provisioner struct {
	// Runner executes the underlying commands. If nil, ExecRunner is used.
	Runner packagemanager.CommandRunner
	// Python is the interpreter used to create the virtualenv. If empty,
	// DefaultPython is used.
	Python string
	// Subdir is the virtualenv directory relative to the release directory. If
	// empty, DefaultSubdir is used.
	Subdir string
}

func (p *Provisioner) runner() packagemanager.CommandRunner {
	if p.Runner != nil {
		return p.Runner
	}
	return packagemanager.ExecRunner{}
}

func (p *Provisioner) python() string {
	if p.Python != "" {
		return p.Python
	}
	return DefaultPython
}

// subdir returns the validated virtualenv subdirectory name.
func (p *Provisioner) subdir() (string, error) {
	sub := p.Subdir
	if sub == "" {
		return DefaultSubdir, nil
	}
	if filepath.IsAbs(sub) || sub == "." || sub == ".." ||
		strings.ContainsRune(sub, '/') || strings.ContainsRune(sub, filepath.Separator) {
		return "", fmt.Errorf("venv: invalid subdir %q: must be a single directory name", sub)
	}
	return sub, nil
}

// VenvDir returns the virtualenv directory for the given release directory.
func (p *Provisioner) VenvDir(targetDir string) (string, error) {
	if targetDir == "" {
		return "", errors.New("venv: target directory must not be empty")
	}
	sub, err := p.subdir()
	if err != nil {
		return "", err
	}
	return filepath.Join(targetDir, sub), nil
}

// VenvPython returns the path to the virtualenv interpreter for targetDir.
func (p *Provisioner) VenvPython(targetDir string) (string, error) {
	dir, err := p.VenvDir(targetDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", "python"), nil
}

// Provision creates a virtualenv under targetDir (if one does not already
// exist) and installs the pinned packages into it. The packages map keys are
// package names and values are exact version pins.
//
// Provision is idempotent: when a usable virtualenv interpreter already exists,
// creation is skipped and pip install is re-run (pip is itself idempotent for
// pinned versions). It returns nil without running any command when packages is
// empty.
func (p *Provisioner) Provision(ctx context.Context, targetDir string, packages map[string]string) error {
	if targetDir == "" {
		return errors.New("venv: target directory must not be empty")
	}

	specs, err := resolvePackages(packages)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return nil
	}

	venvDir, err := p.VenvDir(targetDir)
	if err != nil {
		return err
	}
	venvPython := filepath.Join(venvDir, "bin", "python")

	create, err := needsCreate(venvPython)
	if err != nil {
		return err
	}
	if create {
		if out, rErr := p.runner().Run(ctx, p.python(), "-m", "venv", venvDir); rErr != nil {
			return commandError(p.python(), []string{"-m", "venv", venvDir}, out, rErr)
		}
	}

	args := append([]string{"-m", "pip", "install", "--disable-pip-version-check"}, specs...)
	if out, rErr := p.runner().Run(ctx, venvPython, args...); rErr != nil {
		return commandError(venvPython, args, out, rErr)
	}
	return nil
}

// needsCreate reports whether the virtualenv interpreter must be created. An
// existing interpreter that is not a regular executable file is rejected so a
// stale or corrupt virtualenv surfaces a clear error instead of failing later.
func needsCreate(venvPython string) (bool, error) {
	info, err := os.Stat(venvPython)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("venv: stat interpreter: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return false, fmt.Errorf("venv: existing interpreter is not executable: %s", venvPython)
	}
	return false, nil
}

// resolvePackages converts the name->version map into a sorted slice of
// "name==version" specifiers, validating each name and version.
func resolvePackages(packages map[string]string) ([]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]string, 0, len(names))
	for _, name := range names {
		if err := validateToken("package name", name); err != nil {
			return nil, err
		}
		if strings.Contains(name, "==") {
			return nil, fmt.Errorf("venv: package name must not contain '==': %q", name)
		}
		version := packages[name]
		if err := validateToken("package version", version); err != nil {
			return nil, err
		}
		specs = append(specs, name+"=="+version)
	}
	return specs, nil
}

// validateToken rejects empty values, leading dashes (which could be parsed as
// command options), and whitespace or control characters.
func validateToken(label, value string) error {
	if value == "" {
		return fmt.Errorf("venv: %s must not be empty", label)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("venv: %s must not start with '-': %q", label, value)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("venv: %s must not contain whitespace or control characters: %q", label, value)
		}
	}
	return nil
}

// commandError wraps an execution failure with the command line and any output.
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
