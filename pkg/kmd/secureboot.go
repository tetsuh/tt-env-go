// Package kmd implements safety preflight checks for Tenstorrent kernel-module
// (KMD) operations.
//
// The Secure Boot preflight inspects the UEFI Secure Boot state via mokutil so
// callers (such as a KMD swap flow) can refuse to load or unload kernel modules
// on a system where Secure Boot would block unsigned modules.
package kmd

import (
	"context"
	"os"
	"strings"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// DefaultMokutil is the executable queried for Secure Boot state.
const DefaultMokutil = "mokutil"

// DefaultEFIDir is the sysfs path whose presence indicates a UEFI system.
const DefaultEFIDir = "/sys/firmware/efi"

// SecureBootState is the interpreted Secure Boot status of the host.
type SecureBootState string

const (
	// SecureBootDisabled means Secure Boot is off; KMD operations may proceed.
	SecureBootDisabled SecureBootState = "disabled"
	// SecureBootEnabled means Secure Boot is on; KMD operations must not proceed.
	SecureBootEnabled SecureBootState = "enabled"
	// SecureBootNotApplicable means the host is not a UEFI system, so Secure
	// Boot does not apply and KMD operations may proceed.
	SecureBootNotApplicable SecureBootState = "not_applicable"
	// SecureBootUnavailable means the state could not be queried (for example
	// mokutil is missing or failed to run); callers should not proceed.
	SecureBootUnavailable SecureBootState = "unavailable"
	// SecureBootUnknown means mokutil ran but its output was not recognized;
	// callers should not proceed.
	SecureBootUnknown SecureBootState = "unknown"
)

// SecureBootResult is the typed outcome of the Secure Boot preflight.
type SecureBootResult struct {
	// State is the interpreted Secure Boot status.
	State SecureBootState
	// Detail holds the raw mokutil output (or error text) for diagnostics.
	Detail string
}

// Safe reports whether KMD operations may proceed given the Secure Boot state.
// Only a disabled or non-applicable state is safe; enabled, unavailable, and
// unknown all require the caller to stop.
func (r SecureBootResult) Safe() bool {
	return r.State == SecureBootDisabled || r.State == SecureBootNotApplicable
}

// SecureBootChecker queries and interprets the host Secure Boot state.
type SecureBootChecker struct {
	// Runner executes mokutil. If nil, ExecRunner is used.
	Runner packagemanager.CommandRunner
	// Mokutil is the executable to invoke. If empty, DefaultMokutil is used.
	Mokutil string
	// EFIDir is the path probed to decide whether the host is UEFI. If empty,
	// DefaultEFIDir is used.
	EFIDir string
}

func (c *SecureBootChecker) runner() packagemanager.CommandRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return packagemanager.ExecRunner{}
}

func (c *SecureBootChecker) mokutil() string {
	if c.Mokutil != "" {
		return c.Mokutil
	}
	return DefaultMokutil
}

func (c *SecureBootChecker) efiDir() string {
	if c.EFIDir != "" {
		return c.EFIDir
	}
	return DefaultEFIDir
}

// Check runs the Secure Boot preflight and returns a typed result. It never
// returns an error: every outcome, including a missing mokutil or unparseable
// output, is encoded in the result's State so callers can branch on it.
func (c *SecureBootChecker) Check(ctx context.Context) SecureBootResult {
	// Mirror proto1's `[[ ! -d "$efi_dir" ]]` for the common cases: a missing
	// path or a non-directory means the host is not a UEFI system. Any other
	// stat failure (for example a permission error) leaves the state
	// unqueryable, so fail closed rather than assuming it is safe.
	info, err := os.Stat(c.efiDir())
	switch {
	case os.IsNotExist(err) || (err == nil && !info.IsDir()):
		return SecureBootResult{
			State:  SecureBootNotApplicable,
			Detail: "no UEFI firmware detected at " + c.efiDir(),
		}
	case err != nil:
		return SecureBootResult{
			State:  SecureBootUnavailable,
			Detail: "cannot stat " + c.efiDir() + ": " + err.Error(),
		}
	}

	out, err := c.runner().Run(ctx, c.mokutil(), "--sb-state")
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return SecureBootResult{State: SecureBootUnavailable, Detail: detail}
	}

	raw := strings.TrimSpace(string(out))
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "secureboot disabled"),
		strings.Contains(lower, "secureboot not enabled"):
		return SecureBootResult{State: SecureBootDisabled, Detail: raw}
	case strings.Contains(lower, "secureboot enabled"):
		return SecureBootResult{State: SecureBootEnabled, Detail: raw}
	default:
		return SecureBootResult{State: SecureBootUnknown, Detail: raw}
	}
}
