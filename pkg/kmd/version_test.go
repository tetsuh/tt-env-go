package kmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

func TestVersionProbeNotLoaded(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	p := &VersionProber{
		Runner:       runner,
		Module:       "tenstorrent",
		SysModuleDir: t.TempDir(), // no module subdir
	}
	got := p.Probe(context.Background())
	if got.Loaded {
		t.Errorf("Probe() Loaded = true, want false")
	}
	if len(runner.CommandStrings()) != 0 {
		t.Errorf("modinfo should not run when module not loaded; ran %v", runner.CommandStrings())
	}
}

func TestVersionProbeLoadedWithVersion(t *testing.T) {
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "tenstorrent"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &packagemanager.MockRunner{
		Strict:    true,
		Responses: []packagemanager.CommandResponse{{Output: []byte("1.2.3\n")}},
	}
	p := &VersionProber{Runner: runner, Module: "tenstorrent", SysModuleDir: sys}

	got := p.Probe(context.Background())
	if !got.Loaded {
		t.Fatalf("Probe() Loaded = false, want true")
	}
	if got.Version != "1.2.3" {
		t.Errorf("Probe() Version = %q, want %q", got.Version, "1.2.3")
	}
}

func TestVersionProbeLoadedUnknownOnModinfoError(t *testing.T) {
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "tenstorrent"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &packagemanager.MockRunner{
		Strict:    true,
		Responses: []packagemanager.CommandResponse{{Err: os.ErrNotExist}},
	}
	p := &VersionProber{Runner: runner, Module: "tenstorrent", SysModuleDir: sys}

	got := p.Probe(context.Background())
	if !got.Loaded {
		t.Fatalf("Probe() Loaded = false, want true")
	}
	if got.Version != "" {
		t.Errorf("Probe() Version = %q, want empty (unknown)", got.Version)
	}
}

func TestVersionProbeEnvOverrides(t *testing.T) {
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "ttkmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvModule, "ttkmd")
	t.Setenv(EnvSysModuleDir, sys)

	runner := &packagemanager.MockRunner{
		Strict:    true,
		Responses: []packagemanager.CommandResponse{{Output: []byte("9.9.9\n")}},
	}
	p := &VersionProber{Runner: runner}

	got := p.Probe(context.Background())
	if !got.Loaded || got.Version != "9.9.9" {
		t.Errorf("Probe() = %+v, want loaded 9.9.9", got)
	}
	cmds := runner.CommandStrings()
	if len(cmds) != 1 || !strings.Contains(cmds[0], "ttkmd") {
		t.Errorf("expected modinfo invoked for overridden module, got %v", cmds)
	}
}
