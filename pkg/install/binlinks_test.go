package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/venv"
)

// writeExecutable writes an executable file with the given content.
func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCreateSystemBinLinksVenvWrapper(t *testing.T) {
	staging := t.TempDir()
	// Provide tt-flash from the release venv (tt-flash is required, not a pip
	// command, so the venv path is exercised regardless of pip handling).
	writeExecutable(t, filepath.Join(staging, venv.DefaultSubdir, "bin", "tt-flash"), "#!/bin/sh\n")

	orch := &Orchestrator{Root: t.TempDir(), Logf: func(string, ...any) {},
		LookSystemCommand: func(string) (string, bool) { return "", false }}
	p := &plan{managedCommandNames: map[string]bool{}}
	if err := orch.createSystemBinLinks(staging, p); err != nil {
		t.Fatalf("createSystemBinLinks: %v", err)
	}

	content := readFile(t, filepath.Join(staging, "bin", "tt-flash"))
	if !contains([]string{}, "") && !containsStr(content, "VENV_COMMAND_NAME") {
		t.Errorf("expected venv wrapper, got:\n%s", content)
	}
}

func TestCreateSystemBinLinksSymlink(t *testing.T) {
	staging := t.TempDir()
	// A real system binary to link to.
	sysBin := filepath.Join(t.TempDir(), "tt-flash")
	writeExecutable(t, sysBin, "#!/bin/sh\n")

	orch := &Orchestrator{Root: t.TempDir(), Logf: func(string, ...any) {},
		LookSystemCommand: func(cmd string) (string, bool) {
			if cmd == "tt-flash" {
				return sysBin, true
			}
			return "", false
		}}
	p := &plan{managedCommandNames: map[string]bool{}}
	if err := orch.createSystemBinLinks(staging, p); err != nil {
		t.Fatalf("createSystemBinLinks: %v", err)
	}

	link := filepath.Join(staging, "bin", "tt-flash")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", link, err)
	}
	if target != sysBin {
		t.Errorf("symlink target = %q, want %q", target, sysBin)
	}
}

func TestCreateSystemBinLinksPipAbsoluteWrapper(t *testing.T) {
	staging := t.TempDir()
	// Venv exists but does not provide tt-smi; system tt-smi is a pip command,
	// so an absolute python wrapper must be written.
	if err := os.MkdirAll(filepath.Join(staging, venv.DefaultSubdir, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir venv: %v", err)
	}
	sysBin := filepath.Join(t.TempDir(), "tt-smi")
	writeExecutable(t, sysBin, "#!/bin/sh\n")

	orch := &Orchestrator{Root: t.TempDir(), Logf: func(string, ...any) {},
		LookSystemCommand: func(cmd string) (string, bool) {
			if cmd == "tt-smi" {
				return sysBin, true
			}
			return "", false
		}}
	p := &plan{managedCommandNames: map[string]bool{}}
	if err := orch.createSystemBinLinks(staging, p); err != nil {
		t.Fatalf("createSystemBinLinks: %v", err)
	}

	wrapper := filepath.Join(staging, "bin", "tt-smi")
	if _, err := os.Lstat(wrapper); err != nil {
		t.Fatalf("expected wrapper at %s: %v", wrapper, err)
	}
	if target, err := os.Readlink(wrapper); err == nil {
		t.Fatalf("expected a script wrapper, got symlink to %q", target)
	}
	content := readFile(t, wrapper)
	if !strings.Contains(content, "TARGET_COMMAND") || !strings.Contains(content, sysBin) {
		t.Errorf("expected absolute python wrapper targeting %s, got:\n%s", sysBin, content)
	}
}

func TestCreateSystemBinLinksManagedAndOptionalSkipped(t *testing.T) {
	staging := t.TempDir()
	orch := &Orchestrator{Root: t.TempDir(), Logf: func(string, ...any) {},
		// Pretend everything is available as a system command.
		LookSystemCommand: func(cmd string) (string, bool) {
			sysBin := filepath.Join(staging, "sys-"+cmd)
			writeExecutable(t, sysBin, "#!/bin/sh\n")
			return sysBin, true
		}}
	// tt-flash is git-managed: it must be skipped entirely.
	p := &plan{managedCommandNames: map[string]bool{"tt-flash": true}}
	if err := orch.createSystemBinLinks(staging, p); err != nil {
		t.Fatalf("createSystemBinLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(staging, "bin", "tt-flash")); !os.IsNotExist(err) {
		t.Errorf("managed command tt-flash must not get a bin link")
	}
}

func TestCreateSystemBinLinksOptionalMissingIsSilent(t *testing.T) {
	staging := t.TempDir()
	orch := &Orchestrator{Root: t.TempDir(), Logf: func(string, ...any) {},
		LookSystemCommand: func(string) (string, bool) { return "", false }}
	p := &plan{managedCommandNames: map[string]bool{}}
	if err := orch.createSystemBinLinks(staging, p); err != nil {
		t.Fatalf("createSystemBinLinks must not error when commands are missing: %v", err)
	}
	// No bin links created.
	if _, err := os.Stat(filepath.Join(staging, "bin")); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(filepath.Join(staging, "bin"))
		if len(entries) != 0 {
			t.Errorf("expected no bin links, got %v", entries)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func containsStr(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
