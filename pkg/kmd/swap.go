package kmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// DefaultModule is the Tenstorrent kernel module name swapped by the engine.
const DefaultModule = "tenstorrent"

// DefaultDeviceGlob matches the Tenstorrent device nodes inspected for holders.
const DefaultDeviceGlob = "/dev/tenstorrent/*"

// Environment overrides mirrored from proto1 lib/kmd.sh.
const (
	envModule     = "TT_KMD_MODULE"
	envDeviceGlob = "TT_KMD_DEVICE_GLOB"
)

// Sentinel errors returned by Swap so callers can branch with errors.Is.
var (
	// ErrSecureBootBlocked means the Secure Boot preflight refused the swap.
	ErrSecureBootBlocked = errors.New("kmd: secure boot is not in a safe state")
	// ErrDevicesInUse means Tenstorrent device nodes are held by processes.
	ErrDevicesInUse = errors.New("kmd: tenstorrent devices are in use")
	// ErrSwapFailed means the module failed to (re)load via modprobe.
	ErrSwapFailed = errors.New("kmd: failed to load module")
	// ErrRollbackFailed means modprobe failed and the rollback also failed,
	// leaving the module unloaded.
	ErrRollbackFailed = errors.New("kmd: failed to load module and rollback also failed")
	// ErrNoHolderTool means neither lsof nor fuser is available to inspect
	// device holders.
	ErrNoHolderTool = errors.New("kmd: lsof or fuser is required to inspect device holders")
)

// Holder is a process holding a Tenstorrent device node open.
type Holder struct {
	PID     int
	Command string
}

// SwapResult records the outcome of a swap attempt. It is populated even when
// Swap returns an error so callers can inspect what happened.
type SwapResult struct {
	// Module is the resolved module name.
	Module string
	// SecureBoot is the Secure Boot preflight result.
	SecureBoot SecureBootResult
	// Holders lists processes holding device nodes (set when the swap is
	// blocked by ErrDevicesInUse).
	Holders []Holder
	// WasLoaded reports whether the module was loaded before the swap.
	WasLoaded bool
	// Reloaded reports whether the module was successfully (re)loaded.
	Reloaded bool
	// RolledBack reports whether a failed load was followed by a successful
	// rollback reload of the previously loaded module.
	RolledBack bool
}

// Swapper performs a guarded unload/reload of the Tenstorrent kernel module.
// Mutating commands (rmmod, modprobe) honor Sudo; read-only inspection
// commands (lsmod, lsof, fuser, ps) never use sudo.
type Swapper struct {
	// Runner executes the underlying commands. If nil, ExecRunner is used.
	Runner packagemanager.CommandRunner
	// SecureBoot gates the swap. If nil a default checker sharing Runner is
	// constructed.
	SecureBoot *SecureBootChecker
	// Sudo prefixes mutating commands with sudo when set.
	Sudo bool
	// Module is the kernel module to swap. If empty, the TT_KMD_MODULE
	// environment variable then DefaultModule are used.
	Module string
	// DeviceGlob matches device nodes inspected for holders. If empty, the
	// TT_KMD_DEVICE_GLOB environment variable then DefaultDeviceGlob are used.
	DeviceGlob string
	// Command names; empty values fall back to sensible defaults.
	Lsmod, Rmmod, Modprobe, Lsof, Fuser, Ps string
	// LookPath resolves command availability. If nil, exec.LookPath is used.
	LookPath func(string) (string, error)
	// Glob expands DeviceGlob. If nil, filepath.Glob is used.
	Glob func(string) ([]string, error)
}

// NewSwapper returns a Swapper that runs commands through runner with sudo
// enabled. If runner is nil the production ExecRunner is used.
func NewSwapper(runner packagemanager.CommandRunner) *Swapper {
	if runner == nil {
		runner = packagemanager.ExecRunner{}
	}
	return &Swapper{Runner: runner, Sudo: true}
}

func (s *Swapper) runner() packagemanager.CommandRunner {
	if s.Runner != nil {
		return s.Runner
	}
	return packagemanager.ExecRunner{}
}

func (s *Swapper) lookPath() func(string) (string, error) {
	if s.LookPath != nil {
		return s.LookPath
	}
	return exec.LookPath
}

func (s *Swapper) glob() func(string) ([]string, error) {
	if s.Glob != nil {
		return s.Glob
	}
	return filepath.Glob
}

func (s *Swapper) secureBoot() *SecureBootChecker {
	if s.SecureBoot != nil {
		return s.SecureBoot
	}
	return &SecureBootChecker{Runner: s.Runner}
}

func (s *Swapper) module() string {
	if s.Module != "" {
		return s.Module
	}
	if env := os.Getenv(envModule); env != "" {
		return env
	}
	return DefaultModule
}

func (s *Swapper) deviceGlob() string {
	if s.DeviceGlob != "" {
		return s.DeviceGlob
	}
	if env := os.Getenv(envDeviceGlob); env != "" {
		return env
	}
	return DefaultDeviceGlob
}

func cmdName(field, fallback string) string {
	if field != "" {
		return field
	}
	return fallback
}

// Swap runs the guarded swap sequence: Secure Boot gate, device-holder
// preflight, then unload (if loaded) and reload of the module, rolling back to
// the previously loaded module if the reload fails. The returned SwapResult is
// populated even on error.
func (s *Swapper) Swap(ctx context.Context) (SwapResult, error) {
	module := s.module()
	result := SwapResult{Module: module}

	if err := validateModuleName(module); err != nil {
		return result, err
	}

	// Verify required mutating tools up front for clear errors, matching
	// proto1's _kmd_require_command ordering.
	if err := s.requireCommands(); err != nil {
		return result, err
	}

	sb := s.secureBoot().Check(ctx)
	result.SecureBoot = sb
	if !sb.Safe() {
		return result, fmt.Errorf("%w: %s", ErrSecureBootBlocked, sb.State)
	}

	holders, err := s.Holders(ctx)
	if err != nil {
		return result, err
	}
	if len(holders) > 0 {
		result.Holders = holders
		return result, fmt.Errorf("%w: %s", ErrDevicesInUse, formatHolders(holders))
	}

	loaded, err := s.ModuleLoaded(ctx)
	if err != nil {
		return result, err
	}
	result.WasLoaded = loaded

	if loaded {
		if err := s.runPrivileged(ctx, cmdName(s.Rmmod, "rmmod"), module); err != nil {
			return result, err
		}
	}

	if err := s.runPrivileged(ctx, cmdName(s.Modprobe, "modprobe"), module); err == nil {
		result.Reloaded = true
		return result, nil
	} else if loaded {
		// Reload failed but the module was previously loaded: roll back by
		// reloading the previous module.
		if rbErr := s.runPrivileged(ctx, cmdName(s.Modprobe, "modprobe"), module); rbErr != nil {
			return result, fmt.Errorf("%w: load error: %v; rollback error: %v", ErrRollbackFailed, err, rbErr)
		}
		result.RolledBack = true
		return result, fmt.Errorf("%w: %v", ErrSwapFailed, err)
	} else {
		return result, fmt.Errorf("%w: %v", ErrSwapFailed, err)
	}
}

// requireCommands verifies the mutating/inspection tools needed for a swap are
// available, returning a clear error when one is missing.
func (s *Swapper) requireCommands() error {
	for _, name := range []string{
		cmdName(s.Lsmod, "lsmod"),
		cmdName(s.Rmmod, "rmmod"),
		cmdName(s.Modprobe, "modprobe"),
	} {
		if _, err := s.lookPath()(name); err != nil {
			return fmt.Errorf("kmd: required command %q not found: %w", name, err)
		}
	}
	return nil
}

// runPrivileged runs a mutating command, prefixing sudo when enabled, and wraps
// any failure with the command and its combined output.
func (s *Swapper) runPrivileged(ctx context.Context, name string, args ...string) error {
	if s.Sudo {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	out, err := s.runner().Run(ctx, name, args...)
	if err != nil {
		return commandError(name, args, out, err)
	}
	return nil
}

// ModuleLoaded reports whether the module appears in lsmod output.
func (s *Swapper) ModuleLoaded(ctx context.Context) (bool, error) {
	out, err := s.runner().Run(ctx, cmdName(s.Lsmod, "lsmod"))
	if err != nil {
		return false, commandError(cmdName(s.Lsmod, "lsmod"), nil, out, err)
	}
	module := s.module()
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == module {
			return true, nil
		}
	}
	return false, nil
}

// Holders returns processes holding the Tenstorrent device nodes open. When no
// device nodes exist it returns nil. It prefers lsof and falls back to fuser.
func (s *Swapper) Holders(ctx context.Context) ([]Holder, error) {
	devices, err := s.glob()(s.deviceGlob())
	if err != nil {
		return nil, fmt.Errorf("kmd: cannot expand device glob %q: %w", s.deviceGlob(), err)
	}
	if len(devices) == 0 {
		return nil, nil
	}

	lsof := cmdName(s.Lsof, "lsof")
	fuser := cmdName(s.Fuser, "fuser")
	switch {
	case s.commandAvailable(lsof):
		return s.lsofHolders(ctx, lsof, devices)
	case s.commandAvailable(fuser):
		return s.fuserHolders(ctx, fuser, devices)
	default:
		return nil, ErrNoHolderTool
	}
}

func (s *Swapper) commandAvailable(name string) bool {
	_, err := s.lookPath()(name)
	return err == nil
}

// lsofHolders parses `lsof -F pc -- <devices>` output. lsof exits non-zero when
// no file has a holder, so a non-nil error with empty output is treated as "no
// holders" (matching proto1).
func (s *Swapper) lsofHolders(ctx context.Context, lsof string, devices []string) ([]Holder, error) {
	args := append([]string{"-F", "pc", "--"}, devices...)
	out, err := s.runner().Run(ctx, lsof, args...)
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return nil, nil
	}

	var holders []Holder
	seen := map[int]bool{}
	pid := 0
	havePID := false
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			n, convErr := strconv.Atoi(strings.TrimSpace(line[1:]))
			if convErr == nil {
				pid = n
				havePID = true
			} else {
				havePID = false
			}
		case 'c':
			if havePID && !seen[pid] {
				seen[pid] = true
				command := strings.TrimSpace(line[1:])
				if command == "" {
					command = "unknown"
				}
				holders = append(holders, Holder{PID: pid, Command: command})
			}
		}
	}
	return holders, nil
}

// fuserHolders parses `fuser -- <devices>` output for numeric PIDs and resolves
// each command name via `ps -p <pid> -o comm=`.
func (s *Swapper) fuserHolders(ctx context.Context, fuser string, devices []string) ([]Holder, error) {
	args := append([]string{"--"}, devices...)
	out, err := s.runner().Run(ctx, fuser, args...)
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return nil, nil
	}

	seen := map[int]bool{}
	var pids []int
	for _, token := range strings.Fields(string(out)) {
		n, convErr := strconv.Atoi(token)
		if convErr != nil || seen[n] {
			continue
		}
		seen[n] = true
		pids = append(pids, n)
	}
	sort.Ints(pids)

	var holders []Holder
	for _, pid := range pids {
		holders = append(holders, Holder{PID: pid, Command: s.commandForPID(ctx, pid)})
	}
	return holders, nil
}

func (s *Swapper) commandForPID(ctx context.Context, pid int) string {
	out, err := s.runner().Run(ctx, cmdName(s.Ps, "ps"), "-p", strconv.Itoa(pid), "-o", "comm=")
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(strings.ReplaceAll(string(out), "\n", " "))
	if name == "" {
		return "unknown"
	}
	return name
}

func formatHolders(holders []Holder) string {
	parts := make([]string, len(holders))
	for i, h := range holders {
		parts[i] = fmt.Sprintf("PID %d (%s)", h.PID, h.Command)
	}
	return strings.Join(parts, ", ")
}

// validateModuleName rejects empty names and names that could be misread as
// flags or contain whitespace/control characters.
func validateModuleName(module string) error {
	if module == "" {
		return errors.New("kmd: module name is empty")
	}
	if strings.HasPrefix(module, "-") {
		return fmt.Errorf("kmd: module name %q must not start with '-'", module)
	}
	for _, r := range module {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("kmd: module name %q contains whitespace or control characters", module)
		}
	}
	return nil
}

// commandError wraps a failed command invocation with its combined output.
func commandError(name string, args []string, out []byte, err error) error {
	cmd := name
	if len(args) > 0 {
		cmd = name + " " + strings.Join(args, " ")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return fmt.Errorf("kmd: command %q failed: %w", cmd, err)
	}
	return fmt.Errorf("kmd: command %q failed: %w: %s", cmd, err, trimmed)
}
