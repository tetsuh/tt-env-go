// Package shims generates dispatcher scripts that run tt-env stack commands
// against the active release.
//
// A shim is a small shell script written to ${TT_HOME}/shims/<command>. At run
// time it resolves ${TT_HOME}/current/bin/<command> and execs it, so switching
// the active release (the current symlink) changes what every shim dispatches
// to without regenerating any script.
package shims

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// requiredCommands are always generated.
var requiredCommands = []string{
	"tt-smi",
	"tt-flash",
	"tt-topology",
	"tt-burnin",
}

// optionalCommands are generated too but may legitimately be absent from a
// given release.
var optionalCommands = []string{
	"tt-metalium",
	"tt-metalium-ubuntu22",
	"tt-metalium-ubuntu24",
	"tt-studio",
	"tt-inference-server",
	"tt-metalium-models",
}

// shimTemplate is the dispatcher written for every command. It derives the
// command name from its own filename so a single template serves all shims, and
// resolves TT_HOME and the active release at run time so no path is baked in.
const shimTemplate = `#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${TT_HOME:-}" ]]; then
  if [[ -z "${HOME:-}" ]]; then
    printf '[ERROR] HOME environment variable is not set.\n' >&2
    exit 1
  fi
  TT_HOME="${HOME}/.tt-env"
fi
export TT_HOME

command_name="${0##*/}"
target="${TT_HOME}/current/bin/${command_name}"

if [[ ! -x "$target" ]]; then
  printf '[ERROR] Active tt-env command not found or not executable: %s\n' "$target" >&2
  exit 1
fi

exec "$target" "$@"
`

// KnownCommands returns the sorted list of all shim command names that Generate
// writes by default (required plus optional).
func KnownCommands() []string {
	all := make([]string, 0, len(requiredCommands)+len(optionalCommands))
	all = append(all, requiredCommands...)
	all = append(all, optionalCommands...)
	sort.Strings(all)
	return all
}

// IsOptional reports whether command is an optional shim command.
func IsOptional(command string) bool {
	for _, c := range optionalCommands {
		if c == command {
			return true
		}
	}
	return false
}

// Generator writes and resolves shims under a TT_HOME directory.
type Generator struct {
	// Home is the TT_HOME directory under which shims/ and current/ live.
	Home string
}

// ShimsDir returns the directory holding generated shims (Home/shims).
func (g *Generator) ShimsDir() string {
	return filepath.Join(g.Home, "shims")
}

// currentBin returns the active release's bin directory (Home/current/bin).
func (g *Generator) currentBin() string {
	return filepath.Join(g.Home, "current", "bin")
}

// Generate writes a dispatcher shim for each requested command into ShimsDir,
// making each executable. When no names are given, KnownCommands is used. It
// returns the sorted paths of the shims written.
func (g *Generator) Generate(names ...string) ([]string, error) {
	if g.Home == "" {
		return nil, errors.New("shims: TT_HOME must not be empty")
	}
	if len(names) == 0 {
		names = KnownCommands()
	}
	for _, name := range names {
		if err := validateCommandName(name); err != nil {
			return nil, err
		}
	}

	shimsDir := g.ShimsDir()
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		return nil, fmt.Errorf("shims: create shims dir: %w", err)
	}

	written := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(shimsDir, name)
		if err := writeShim(shimsDir, path); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	sort.Strings(written)
	return written, nil
}

// Resolve returns the path the named command's shim dispatches to for the active
// release (Home/current/bin/<command>) and verifies it is an executable regular
// file. It mirrors the resolution the generated shim performs at run time, so it
// reflects the active release without regenerating shims.
func (g *Generator) Resolve(command string) (string, error) {
	if g.Home == "" {
		return "", errors.New("shims: TT_HOME must not be empty")
	}
	if err := validateCommandName(command); err != nil {
		return "", err
	}
	target := filepath.Join(g.currentBin(), command)
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("shims: %s is not available in the active release: %s", command, target)
		}
		return "", fmt.Errorf("shims: stat %s: %w", target, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("shims: %s is not executable: %s", command, target)
	}
	return target, nil
}

// writeShim writes the dispatcher script to path atomically and makes it
// executable. The script is written to a fresh exclusive temp file in dir and
// renamed into place so an existing path or temp symlink cannot be clobbered.
func writeShim(dir, path string) error {
	f, err := os.CreateTemp(dir, ".shim-*.tmp")
	if err != nil {
		return fmt.Errorf("shims: create temp for %s: %w", path, err)
	}
	tmp := f.Name()
	if _, err := f.WriteString(shimTemplate); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("shims: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("shims: write %s: %w", path, err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("shims: chmod %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("shims: install %s: %w", path, err)
	}
	return nil
}

// validateCommandName ensures command is a single, safe path element.
func validateCommandName(command string) error {
	if command == "" {
		return errors.New("shims: command name must not be empty")
	}
	if filepath.IsAbs(command) || command == "." || command == ".." ||
		strings.ContainsRune(command, '/') || strings.ContainsRune(command, filepath.Separator) {
		return fmt.Errorf("shims: invalid command name %q: must be a single name", command)
	}
	if strings.HasPrefix(command, "-") {
		return fmt.Errorf("shims: command name must not start with '-': %q", command)
	}
	for _, r := range command {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("shims: command name must not contain whitespace or control characters: %q", command)
		}
	}
	return nil
}
