package kmd

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// toolsPresent returns a LookPath stub reporting the named commands as found.
func toolsPresent(names ...string) func(string) (string, error) {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

// noDevices is a Glob stub reporting no device nodes.
func noDevices(string) ([]string, error) { return nil, nil }

// oneDevice is a Glob stub reporting a single Tenstorrent device node.
func oneDevice(string) ([]string, error) { return []string{"/dev/tenstorrent/0"}, nil }

// safeSecureBoot returns a checker that reports not_applicable (no UEFI), which
// is Safe and consumes no runner command.
func safeSecureBoot(t *testing.T, runner packagemanager.CommandRunner) *SecureBootChecker {
	t.Helper()
	return &SecureBootChecker{Runner: runner, EFIDir: filepath.Join(t.TempDir(), "no-efi")}
}

func newTestSwapper(runner packagemanager.CommandRunner) *Swapper {
	return &Swapper{
		Runner:   runner,
		Sudo:     true,
		LookPath: toolsPresent("lsmod", "rmmod", "modprobe", "lsof", "fuser", "ps"),
		Glob:     noDevices,
	}
}

func TestSwapLoadedModuleReloads(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("Module Size Used\ntenstorrent 123456 0\n")},
		{}, // rmmod
		{}, // modprobe
	}}
	s := newTestSwapper(runner)
	s.SecureBoot = safeSecureBoot(t, runner)

	res, err := s.Swap(context.Background())
	if err != nil {
		t.Fatalf("Swap() error = %v", err)
	}
	if !res.WasLoaded || !res.Reloaded || res.RolledBack {
		t.Errorf("result = %+v, want WasLoaded && Reloaded && !RolledBack", res)
	}
	want := []string{"lsmod", "sudo rmmod tenstorrent", "sudo modprobe tenstorrent"}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestSwapNotLoadedSkipsRmmod(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("Module Size Used\nother_mod 10 0\n")},
		{}, // modprobe
	}}
	s := newTestSwapper(runner)
	s.SecureBoot = safeSecureBoot(t, runner)

	res, err := s.Swap(context.Background())
	if err != nil {
		t.Fatalf("Swap() error = %v", err)
	}
	if res.WasLoaded || !res.Reloaded {
		t.Errorf("result = %+v, want !WasLoaded && Reloaded", res)
	}
	want := []string{"lsmod", "sudo modprobe tenstorrent"}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestSwapNoSudo(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("tenstorrent 1 0\n")},
		{}, // rmmod
		{}, // modprobe
	}}
	s := newTestSwapper(runner)
	s.Sudo = false
	s.SecureBoot = safeSecureBoot(t, runner)

	if _, err := s.Swap(context.Background()); err != nil {
		t.Fatalf("Swap() error = %v", err)
	}
	want := []string{"lsmod", "rmmod tenstorrent", "modprobe tenstorrent"}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestSwapBlockedByHoldersLsof(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("p1234\nctt-smi\np1234\ncignored-dup\n")},
	}}
	s := newTestSwapper(runner)
	s.Glob = oneDevice
	s.SecureBoot = safeSecureBoot(t, runner)

	res, err := s.Swap(context.Background())
	if !errors.Is(err, ErrDevicesInUse) {
		t.Fatalf("error = %v, want ErrDevicesInUse", err)
	}
	if len(res.Holders) != 1 || res.Holders[0].PID != 1234 || res.Holders[0].Command != "tt-smi" {
		t.Errorf("holders = %+v, want one {1234 tt-smi}", res.Holders)
	}
	want := []string{"lsof -F pc -- /dev/tenstorrent/0"}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v (rmmod/modprobe must not run)", got, want)
	}
}

func TestSwapHoldersFuserFallback(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("/dev/tenstorrent/0: 5678 1234\n")},
		{Output: []byte("tt-smi\n")},  // ps -p 1234
		{Output: []byte("python3\n")}, // ps -p 5678
	}}
	s := newTestSwapper(runner)
	s.Glob = oneDevice
	s.LookPath = toolsPresent("lsmod", "rmmod", "modprobe", "fuser", "ps") // no lsof
	s.SecureBoot = safeSecureBoot(t, runner)

	res, err := s.Swap(context.Background())
	if !errors.Is(err, ErrDevicesInUse) {
		t.Fatalf("error = %v, want ErrDevicesInUse", err)
	}
	if len(res.Holders) != 2 || res.Holders[0].PID != 1234 || res.Holders[1].PID != 5678 {
		t.Errorf("holders = %+v, want sorted PIDs 1234,5678", res.Holders)
	}
	want := []string{
		"fuser -- /dev/tenstorrent/0",
		"ps -p 1234 -o comm=",
		"ps -p 5678 -o comm=",
	}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestSwapNoHolderTool(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	s := newTestSwapper(runner)
	s.Glob = oneDevice
	s.LookPath = toolsPresent("lsmod", "rmmod", "modprobe") // no lsof/fuser
	s.SecureBoot = safeSecureBoot(t, runner)

	if _, err := s.Swap(context.Background()); !errors.Is(err, ErrNoHolderTool) {
		t.Fatalf("error = %v, want ErrNoHolderTool", err)
	}
}

func TestSwapBlockedBySecureBoot(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("SecureBoot enabled\n")},
	}}
	s := newTestSwapper(runner)
	s.SecureBoot = &SecureBootChecker{Runner: runner, EFIDir: t.TempDir()}

	res, err := s.Swap(context.Background())
	if !errors.Is(err, ErrSecureBootBlocked) {
		t.Fatalf("error = %v, want ErrSecureBootBlocked", err)
	}
	if res.SecureBoot.State != SecureBootEnabled {
		t.Errorf("SecureBoot.State = %q, want enabled", res.SecureBoot.State)
	}
	want := []string{"mokutil --sb-state"}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v (no rmmod/modprobe)", got, want)
	}
}

func TestSwapModprobeFailsRollsBack(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("tenstorrent 1 0\n")},
		{},                                 // rmmod ok
		{Err: errors.New("exit status 1")}, // modprobe fails
		{},                                 // rollback modprobe ok
	}}
	s := newTestSwapper(runner)
	s.SecureBoot = safeSecureBoot(t, runner)

	res, err := s.Swap(context.Background())
	if !errors.Is(err, ErrSwapFailed) {
		t.Fatalf("error = %v, want ErrSwapFailed", err)
	}
	if !res.RolledBack || res.Reloaded {
		t.Errorf("result = %+v, want RolledBack && !Reloaded", res)
	}
	want := []string{"lsmod", "sudo rmmod tenstorrent", "sudo modprobe tenstorrent", "sudo modprobe tenstorrent"}
	if got := runner.CommandStrings(); !equalStrings(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestSwapRollbackAlsoFails(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("tenstorrent 1 0\n")},
		{},                                 // rmmod ok
		{Err: errors.New("exit status 1")}, // modprobe fails
		{Err: errors.New("exit status 1")}, // rollback fails
	}}
	s := newTestSwapper(runner)
	s.SecureBoot = safeSecureBoot(t, runner)

	if _, err := s.Swap(context.Background()); !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("error = %v, want ErrRollbackFailed", err)
	}
}

func TestSwapModprobeFailsNotLoaded(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("other 1 0\n")},
		{Err: errors.New("exit status 1")}, // modprobe fails, nothing to roll back
	}}
	s := newTestSwapper(runner)
	s.SecureBoot = safeSecureBoot(t, runner)

	res, err := s.Swap(context.Background())
	if !errors.Is(err, ErrSwapFailed) {
		t.Fatalf("error = %v, want ErrSwapFailed", err)
	}
	if res.RolledBack {
		t.Errorf("RolledBack = true, want false when module was not loaded")
	}
}

func TestSwapRejectsBadModuleName(t *testing.T) {
	for _, name := range []string{"", "-evil", "bad name", "tab\tname"} {
		s := &Swapper{Module: name, Runner: &packagemanager.MockRunner{Strict: true}}
		if _, err := s.Swap(context.Background()); err == nil {
			t.Errorf("Swap() with module %q = nil error, want validation error", name)
		}
	}
}

func TestSwapRequiresCommands(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	s := newTestSwapper(runner)
	s.LookPath = toolsPresent("lsmod", "modprobe") // rmmod missing

	if _, err := s.Swap(context.Background()); err == nil {
		t.Fatal("Swap() = nil error, want required-command error")
	}
}

func TestModuleLoaded(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"loaded", "Module Size Used\ntenstorrent 123 0\n", true},
		{"not loaded", "Module Size Used\nother 1 0\n", false},
		{"substring not matched", "Module Size Used\ntenstorrent_x 1 0\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
				{Output: []byte(tc.output)},
			}}
			s := &Swapper{Runner: runner}
			got, err := s.ModuleLoaded(context.Background())
			if err != nil {
				t.Fatalf("ModuleLoaded() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("ModuleLoaded() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHoldersNoDevices(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	s := newTestSwapper(runner) // Glob = noDevices

	holders, err := s.Holders(context.Background())
	if err != nil {
		t.Fatalf("Holders() error = %v", err)
	}
	if holders != nil {
		t.Errorf("Holders() = %v, want nil when no device nodes", holders)
	}
	if got := runner.CommandStrings(); len(got) != 0 {
		t.Errorf("no holder command should run, ran %v", got)
	}
}

func TestHoldersLsofNoHolders(t *testing.T) {
	// lsof exits non-zero with empty output when nothing holds the files.
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Err: errors.New("exit status 1")},
	}}
	s := newTestSwapper(runner)
	s.Glob = oneDevice

	holders, err := s.Holders(context.Background())
	if err != nil {
		t.Fatalf("Holders() error = %v", err)
	}
	if holders != nil {
		t.Errorf("Holders() = %v, want nil", holders)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
