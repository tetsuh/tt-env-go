package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tetsuh/tt-env-go/pkg/shims"
	"github.com/tetsuh/tt-env-go/pkg/venv"
)

// createSystemBinLinks creates bin/<command> entries for each known shim command
// that is provided by the host system or the release virtualenv, mirroring
// proto1's _install_create_system_bin_links. Commands provided by git or
// container components are skipped (those already have wrappers). For each
// remaining command it prefers, in order: a venv-provided command (venv python
// wrapper); a system command that is also a pip command when a venv exists
// (absolute python wrapper); otherwise a plain symlink to the system command.
// Required commands that resolve to nothing produce a warning, optional ones are
// skipped silently.
func (o *Orchestrator) createSystemBinLinks(stagingDir string, p *plan) error {
	binDir := filepath.Join(stagingDir, "bin")
	venvDir := filepath.Join(stagingDir, venv.DefaultSubdir)
	venvBinDir := filepath.Join(venvDir, "bin")
	venvExists := dirExists(venvDir)

	for _, command := range shims.KnownCommands() {
		if p.managedCommandNames[command] {
			continue
		}

		venvCommandPath := filepath.Join(venvBinDir, command)
		if isExecutableFile(venvCommandPath) {
			content, err := renderVenvPythonWrapper(command, venv.DefaultSubdir)
			if err != nil {
				return err
			}
			if err := writeWrapper(binDir, command, content); err != nil {
				return err
			}
			o.logf("Created venv bin link for %s", command)
			continue
		}

		systemPath, ok := o.lookSystemCommandFn()(command)
		if !ok {
			if !shims.IsOptional(command) {
				o.logf("[WARNING] command %s not found in system directories; skipping bin link", command)
			}
			continue
		}

		if pipPackageCommands[command] && venvExists {
			content, err := renderAbsolutePythonWrapper(systemPath, venv.DefaultSubdir)
			if err != nil {
				return err
			}
			if err := writeWrapper(binDir, command, content); err != nil {
				return err
			}
			o.logf("Created python bin link for %s -> %s", command, systemPath)
			continue
		}

		if err := symlinkForce(binDir, command, systemPath); err != nil {
			return err
		}
		o.logf("Linked system command %s -> %s", command, systemPath)
	}
	return nil
}

// lookSystemCommandFn returns the configured system-command lookup, defaulting
// to the preferred-directory search.
func (o *Orchestrator) lookSystemCommandFn() func(string) (string, bool) {
	if o.LookSystemCommand != nil {
		return o.LookSystemCommand
	}
	return o.defaultLookSystemCommand
}

// defaultLookSystemCommand searches the preferred system directories for an
// executable command, skipping any path that resolves to a tt-managed location
// under Root.
func (o *Orchestrator) defaultLookSystemCommand(command string) (string, bool) {
	var rootReal string
	if r, err := filepath.EvalSymlinks(o.Root); err == nil {
		rootReal = r
	} else {
		rootReal = o.Root
	}

	for _, dir := range preferredSystemCommandDirs {
		candidate := filepath.Join(dir, command)
		if !isExecutableFile(candidate) {
			continue
		}
		if isUnder(candidate, o.Root) || isUnder(candidate, rootReal) {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			if isUnder(resolved, o.Root) || isUnder(resolved, rootReal) {
				continue
			}
		}
		return candidate, true
	}
	return "", false
}

// symlinkForce creates binDir/<name> as a symlink to target, replacing any
// existing entry, emulating `ln -sf`.
func symlinkForce(binDir, name, target string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("install: create bin directory: %w", err)
	}
	link := filepath.Join(binDir, name)
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("install: replace bin link %s: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("install: create bin link %s: %w", link, err)
	}
	return nil
}

// isExecutableFile reports whether path is a regular file with an execute bit.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isUnder reports whether target is base or lies within the base directory.
func isUnder(target, base string) bool {
	if base == "" {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
