package venv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

func wantCommands(t *testing.T, runner *packagemanager.MockRunner, want []string) {
	t.Helper()
	got := runner.CommandStrings()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands =\n  %#v\nwant\n  %#v", got, want)
	}
}

// fabricateVenvPython creates an executable file at the venv interpreter path to
// simulate an existing virtualenv without creating a real one.
func fabricateVenvPython(t *testing.T, venvPython string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(venvPython), 0o755); err != nil {
		t.Fatalf("mkdir venv bin: %v", err)
	}
	if err := os.WriteFile(venvPython, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write venv python: %v", err)
	}
}

func TestProvisionFresh(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{{}, {}}}
	p := &Provisioner{Runner: runner}

	err := p.Provision(context.Background(), dir, map[string]string{
		"textual": "0.59.0",
		"tt-smi":  "5.2.0",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	venv := filepath.Join(dir, "venv")
	venvPython := filepath.Join(venv, "bin", "python")
	wantCommands(t, runner, []string{
		"python3 -m venv " + venv,
		venvPython + " -m pip install --disable-pip-version-check textual==0.59.0 tt-smi==5.2.0",
	})
}

func TestProvisionReRunSkipsCreate(t *testing.T) {
	dir := t.TempDir()
	venvPython := filepath.Join(dir, "venv", "bin", "python")
	fabricateVenvPython(t, venvPython)

	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{{}}}
	p := &Provisioner{Runner: runner}

	if err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": "5.2.0"}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Only the pip install command runs; venv creation is skipped.
	wantCommands(t, runner, []string{
		venvPython + " -m pip install --disable-pip-version-check tt-smi==5.2.0",
	})
}

func TestProvisionNoPackages(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true}
	p := &Provisioner{Runner: runner}

	if err := p.Provision(context.Background(), dir, nil); err != nil {
		t.Fatalf("Provision(nil): %v", err)
	}
	if err := p.Provision(context.Background(), dir, map[string]string{}); err != nil {
		t.Fatalf("Provision(empty): %v", err)
	}
	wantCommands(t, runner, nil)
}

func TestProvisionCustomPythonAndSubdir(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{{}, {}}}
	p := &Provisioner{Runner: runner, Python: "python3.12", Subdir: ".venv"}

	if err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": "5.2.0"}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	venv := filepath.Join(dir, ".venv")
	venvPython := filepath.Join(venv, "bin", "python")
	wantCommands(t, runner, []string{
		"python3.12 -m venv " + venv,
		venvPython + " -m pip install --disable-pip-version-check tt-smi==5.2.0",
	})
}

func TestProvisionMissingVersion(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true}
	p := &Provisioner{Runner: runner}

	err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": ""})
	if err == nil {
		t.Fatalf("Provision: expected error for missing version")
	}
	wantCommands(t, runner, nil)
}

func TestProvisionInvalidTokens(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]map[string]string{
		"empty name":      {"": "1.0"},
		"option name":     {"--evil": "1.0"},
		"name with eqeq":  {"a==b": "1.0"},
		"option version":  {"pkg": "-1.0"},
		"space in name":   {"bad name": "1.0"},
		"space version":   {"pkg": "1.0 rc1"},
		"control version": {"pkg": "1.0\n"},
	}
	for name, pkgs := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &packagemanager.MockRunner{Strict: true}
			p := &Provisioner{Runner: runner}
			if err := p.Provision(context.Background(), dir, pkgs); err == nil {
				t.Fatalf("Provision(%v): expected error", pkgs)
			}
			wantCommands(t, runner, nil)
		})
	}
}

func TestProvisionEmptyTargetDir(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	p := &Provisioner{Runner: runner}
	if err := p.Provision(context.Background(), "", map[string]string{"tt-smi": "5.2.0"}); err == nil {
		t.Fatalf("Provision: expected error for empty targetDir")
	}
	wantCommands(t, runner, nil)
}

func TestProvisionInvalidSubdir(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"..", ".", "a/b", "/abs"} {
		t.Run(sub, func(t *testing.T) {
			runner := &packagemanager.MockRunner{Strict: true}
			p := &Provisioner{Runner: runner, Subdir: sub}
			if err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": "5.2.0"}); err == nil {
				t.Fatalf("Provision: expected error for subdir %q", sub)
			}
			wantCommands(t, runner, nil)
		})
	}
}

func TestProvisionExistingInterpreterNotExecutable(t *testing.T) {
	dir := t.TempDir()
	venvPython := filepath.Join(dir, "venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(venvPython), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(venvPython, []byte("x"), 0o644); err != nil { // not executable
		t.Fatalf("write: %v", err)
	}

	runner := &packagemanager.MockRunner{Strict: true}
	p := &Provisioner{Runner: runner}
	if err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": "5.2.0"}); err == nil {
		t.Fatalf("Provision: expected error for non-executable interpreter")
	}
	wantCommands(t, runner, nil)
}

func TestProvisionCreateError(t *testing.T) {
	dir := t.TempDir()
	sentinel := errors.New("venv create failed")
	runner := &packagemanager.MockRunner{
		Strict:    true,
		Responses: []packagemanager.CommandResponse{{Err: sentinel}},
	}
	p := &Provisioner{Runner: runner}

	err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": "5.2.0"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Provision err = %v, want wrap of sentinel", err)
	}
	// pip install must not run after a failed create.
	wantCommands(t, runner, []string{"python3 -m venv " + filepath.Join(dir, "venv")})
}

func TestProvisionPipError(t *testing.T) {
	dir := t.TempDir()
	sentinel := errors.New("pip failed")
	runner := &packagemanager.MockRunner{
		Strict:    true,
		Responses: []packagemanager.CommandResponse{{}, {Output: []byte("ERROR: no match"), Err: sentinel}},
	}
	p := &Provisioner{Runner: runner}

	err := p.Provision(context.Background(), dir, map[string]string{"tt-smi": "5.2.0"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Provision err = %v, want wrap of sentinel", err)
	}
}

func TestVenvPathHelpers(t *testing.T) {
	p := &Provisioner{}
	dir := "/opt/tt/versions/v1"

	gotDir, err := p.VenvDir(dir)
	if err != nil {
		t.Fatalf("VenvDir: %v", err)
	}
	if want := filepath.Join(dir, "venv"); gotDir != want {
		t.Errorf("VenvDir = %q, want %q", gotDir, want)
	}

	gotPy, err := p.VenvPython(dir)
	if err != nil {
		t.Fatalf("VenvPython: %v", err)
	}
	if want := filepath.Join(dir, "venv", "bin", "python"); gotPy != want {
		t.Errorf("VenvPython = %q, want %q", gotPy, want)
	}

	if _, err := p.VenvDir(""); err == nil {
		t.Errorf("VenvDir(\"\"): expected error")
	}
}

func TestProvisionLatestFresh(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{{}, {}}}
	p := &Provisioner{Runner: runner}

	if err := p.ProvisionLatest(context.Background(), dir, []string{"tt-smi", "textual"}); err != nil {
		t.Fatalf("ProvisionLatest: %v", err)
	}

	venv := filepath.Join(dir, "venv")
	venvPython := filepath.Join(venv, "bin", "python")
	wantCommands(t, runner, []string{
		"python3 -m venv " + venv,
		venvPython + " -m pip install --disable-pip-version-check textual tt-smi",
	})
}

func TestProvisionLatestSortsAndNoPackages(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{{}, {}}}
	p := &Provisioner{Runner: runner}

	if err := p.ProvisionLatest(context.Background(), dir, []string{"zlib", "alpha"}); err != nil {
		t.Fatalf("ProvisionLatest: %v", err)
	}
	venvPython := filepath.Join(dir, "venv", "bin", "python")
	wantCommands(t, runner, []string{
		"python3 -m venv " + filepath.Join(dir, "venv"),
		venvPython + " -m pip install --disable-pip-version-check alpha zlib",
	})

	emptyRunner := &packagemanager.MockRunner{Strict: true}
	pe := &Provisioner{Runner: emptyRunner}
	if err := pe.ProvisionLatest(context.Background(), t.TempDir(), nil); err != nil {
		t.Fatalf("ProvisionLatest(empty): %v", err)
	}
	wantCommands(t, emptyRunner, nil)
}

func TestProvisionLatestRejectsBadName(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	p := &Provisioner{Runner: runner}

	if err := p.ProvisionLatest(context.Background(), t.TempDir(), []string{"--evil"}); err == nil {
		t.Fatalf("ProvisionLatest: expected error for invalid name")
	}
	wantCommands(t, runner, nil)
}
