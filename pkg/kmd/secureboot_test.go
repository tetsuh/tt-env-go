package kmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// efiPresent returns an EFIDir path that exists, forcing the mokutil code path.
func efiPresent(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestCheckSecureBootDisabled(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("SecureBoot disabled\n")},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t)}

	res := c.Check(context.Background())
	if res.State != SecureBootDisabled {
		t.Fatalf("State = %q, want disabled", res.State)
	}
	if !res.Safe() {
		t.Errorf("Safe() = false, want true for disabled")
	}
	if got := runner.CommandStrings(); len(got) != 1 || got[0] != "mokutil --sb-state" {
		t.Errorf("commands = %v, want [mokutil --sb-state]", got)
	}
}

func TestCheckSecureBootNotEnabled(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("SecureBoot not enabled\n")},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t)}

	res := c.Check(context.Background())
	if res.State != SecureBootDisabled {
		t.Fatalf("State = %q, want disabled", res.State)
	}
	if !res.Safe() {
		t.Errorf("Safe() = false, want true")
	}
}

func TestCheckSecureBootEnabled(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("SecureBoot enabled\n")},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t)}

	res := c.Check(context.Background())
	if res.State != SecureBootEnabled {
		t.Fatalf("State = %q, want enabled", res.State)
	}
	if res.Safe() {
		t.Errorf("Safe() = true, want false for enabled")
	}
}

func TestCheckMokutilAbsent(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Err: errors.New(`exec: "mokutil": executable file not found in $PATH`)},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t)}

	res := c.Check(context.Background())
	if res.State != SecureBootUnavailable {
		t.Fatalf("State = %q, want unavailable", res.State)
	}
	if res.Safe() {
		t.Errorf("Safe() = true, want false for unavailable")
	}
	if res.Detail == "" {
		t.Errorf("Detail should carry diagnostic text")
	}
}

func TestCheckMokutilErrorUsesOutput(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("EFI variables are not supported on this system"), Err: errors.New("exit status 255")},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t)}

	res := c.Check(context.Background())
	if res.State != SecureBootUnavailable {
		t.Fatalf("State = %q, want unavailable", res.State)
	}
	if res.Detail != "EFI variables are not supported on this system" {
		t.Errorf("Detail = %q, want mokutil output", res.Detail)
	}
}

func TestCheckUnparseableOutput(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("something unexpected")},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t)}

	res := c.Check(context.Background())
	if res.State != SecureBootUnknown {
		t.Fatalf("State = %q, want unknown", res.State)
	}
	if res.Safe() {
		t.Errorf("Safe() = true, want false for unknown")
	}
}

func TestCheckNonEFISystemSkipsMokutil(t *testing.T) {
	// EFIDir does not exist -> not a UEFI system.
	missing := filepath.Join(t.TempDir(), "no-efi")
	runner := &packagemanager.MockRunner{Strict: true}
	c := &SecureBootChecker{Runner: runner, EFIDir: missing}

	res := c.Check(context.Background())
	if res.State != SecureBootNotApplicable {
		t.Fatalf("State = %q, want not_applicable", res.State)
	}
	if !res.Safe() {
		t.Errorf("Safe() = false, want true for non-EFI")
	}
	if got := runner.CommandStrings(); len(got) != 0 {
		t.Errorf("mokutil should not run on non-EFI host, ran %v", got)
	}
}

func TestCheckEFIDirStatErrorFailsClosed(t *testing.T) {
	// A non-directory parent makes os.Stat on the child return a non-NotExist
	// error (ENOTDIR), standing in for an unqueryable /sys/firmware/efi.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	efiDir := filepath.Join(file, "efi")
	runner := &packagemanager.MockRunner{Strict: true}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiDir}

	res := c.Check(context.Background())
	if res.State != SecureBootUnavailable {
		t.Fatalf("State = %q, want unavailable", res.State)
	}
	if res.Safe() {
		t.Errorf("Safe() = true, want false for unqueryable EFI dir")
	}
	if got := runner.CommandStrings(); len(got) != 0 {
		t.Errorf("mokutil should not run when EFI dir is unqueryable, ran %v", got)
	}
}

func TestCheckCustomMokutilPath(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("SecureBoot disabled")},
	}}
	c := &SecureBootChecker{Runner: runner, EFIDir: efiPresent(t), Mokutil: "/usr/bin/mokutil"}

	res := c.Check(context.Background())
	if res.State != SecureBootDisabled {
		t.Fatalf("State = %q, want disabled", res.State)
	}
	if got := runner.CommandStrings(); len(got) != 1 || got[0] != "/usr/bin/mokutil --sb-state" {
		t.Errorf("commands = %v, want [/usr/bin/mokutil --sb-state]", got)
	}
}

func TestCheckNonDirectoryEFIPathSkipsMokutil(t *testing.T) {
	// EFIDir exists but is a regular file -> proto1 treats this as non-UEFI.
	f := filepath.Join(t.TempDir(), "efi-as-file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &packagemanager.MockRunner{Strict: true}
	c := &SecureBootChecker{Runner: runner, EFIDir: f}

	res := c.Check(context.Background())
	if res.State != SecureBootNotApplicable {
		t.Fatalf("State = %q, want not_applicable", res.State)
	}
	if got := runner.CommandStrings(); len(got) != 0 {
		t.Errorf("mokutil should not run when EFIDir is not a directory, ran %v", got)
	}
}
