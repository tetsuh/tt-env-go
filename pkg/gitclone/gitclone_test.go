package gitclone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

const (
	studioURL = "https://github.com/tenstorrent/tt-studio.git"
	studioSHA = "a6d347af3980540bb16d10ec473a6b09ce6f2138"
	inferURL  = "https://github.com/tenstorrent/tt-inference-server.git"
	inferSHA  = "b1c2d3e4f5061728394a5b6c7d8e9f0011223344"
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

func TestProvisionFreshClone(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{},                          // clone
		{},                          // fetch
		{},                          // checkout
		{Output: []byte(studioSHA)}, // rev-parse HEAD
		{Output: []byte(studioSHA)}, // rev-parse --verify <sha>^{commit}
	}}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	compDir := filepath.Join(dir, "tt-studio")
	wantCommands(t, runner, []string{
		"git clone --filter=blob:none -- " + studioURL + " " + compDir,
		"git -C " + compDir + " fetch origin",
		"git -C " + compDir + " checkout --detach " + studioSHA,
		"git -C " + compDir + " rev-parse HEAD",
		"git -C " + compDir + " rev-parse --verify " + studioSHA + "^{commit}",
	})
}

func TestProvisionMultipleSorted(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{}, {}, {}, {Output: []byte(inferSHA)}, {Output: []byte(inferSHA)}, // tt-inference-server first (sorted)
		{}, {}, {}, {Output: []byte(studioSHA)}, {Output: []byte(studioSHA)},
	}}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio":           {URL: studioURL, Version: studioSHA},
		"tt-inference-server": {URL: inferURL, Version: inferSHA},
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	inferDir := filepath.Join(dir, "tt-inference-server")
	studioDir := filepath.Join(dir, "tt-studio")
	wantCommands(t, runner, []string{
		"git clone --filter=blob:none -- " + inferURL + " " + inferDir,
		"git -C " + inferDir + " fetch origin",
		"git -C " + inferDir + " checkout --detach " + inferSHA,
		"git -C " + inferDir + " rev-parse HEAD",
		"git -C " + inferDir + " rev-parse --verify " + inferSHA + "^{commit}",
		"git clone --filter=blob:none -- " + studioURL + " " + studioDir,
		"git -C " + studioDir + " fetch origin",
		"git -C " + studioDir + " checkout --detach " + studioSHA,
		"git -C " + studioDir + " rev-parse HEAD",
		"git -C " + studioDir + " rev-parse --verify " + studioSHA + "^{commit}",
	})
}

func TestProvisionExistingMatchingSkipsClone(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "tt-studio")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte(studioURL)}, // remote get-url origin
		{},                          // fetch
		{},                          // checkout
		{Output: []byte(studioSHA)}, // rev-parse HEAD
		{Output: []byte(studioSHA)}, // rev-parse --verify
	}}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// No clone command; origin is checked instead.
	wantCommands(t, runner, []string{
		"git -C " + compDir + " remote get-url origin",
		"git -C " + compDir + " fetch origin",
		"git -C " + compDir + " checkout --detach " + studioSHA,
		"git -C " + compDir + " rev-parse HEAD",
		"git -C " + compDir + " rev-parse --verify " + studioSHA + "^{commit}",
	})
}

func TestProvisionExistingNormalizedURLMatch(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "tt-studio")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("https://github.com/tenstorrent/tt-studio")}, // no .git suffix
		{}, {}, {Output: []byte(studioSHA)}, {Output: []byte(studioSHA)},
	}}
	c := &Cloner{Runner: runner}

	if err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
}

func TestProvisionExistingURLMismatch(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "tt-studio")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("https://github.com/evil/tt-studio.git")},
	}}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	})
	if err == nil {
		t.Fatalf("Provision: expected error on origin mismatch")
	}
	// Only the remote check ran; no fetch/checkout.
	wantCommands(t, runner, []string{"git -C " + compDir + " remote get-url origin"})
}

func TestProvisionExistingNotGitRepo(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "tt-studio")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Err: errors.New("not a git repository")},
	}}
	c := &Cloner{Runner: runner}

	if err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	}); err == nil {
		t.Fatalf("Provision: expected error for non-git directory")
	}
}

func TestProvisionHeadMismatch(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{}, {}, {},
		{Output: []byte("0000000000000000000000000000000000000000")}, // HEAD
		{Output: []byte(studioSHA)},                                  // resolved pin
	}}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	})
	if err == nil {
		t.Fatalf("Provision: expected error on HEAD mismatch")
	}
}

func TestProvisionTagResolvesToCommit(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{}, {}, {},
		{Output: []byte(studioSHA)}, // HEAD
		{Output: []byte(studioSHA)}, // v1.0.0^{commit} resolves to same SHA
	}}
	c := &Cloner{Runner: runner}

	if err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: "v1.0.0"},
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	compDir := filepath.Join(dir, "tt-studio")
	got := runner.CommandStrings()
	wantLast := "git -C " + compDir + " rev-parse --verify v1.0.0^{commit}"
	if got[len(got)-1] != wantLast {
		t.Errorf("last command = %q, want %q", got[len(got)-1], wantLast)
	}
}

func TestProvisionCustomGit(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{}, {}, {}, {Output: []byte(studioSHA)}, {Output: []byte(studioSHA)},
	}}
	c := &Cloner{Runner: runner, Git: "/usr/bin/git"}

	if err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	compDir := filepath.Join(dir, "tt-studio")
	wantCommands(t, runner, []string{
		"/usr/bin/git clone --filter=blob:none -- " + studioURL + " " + compDir,
		"/usr/bin/git -C " + compDir + " fetch origin",
		"/usr/bin/git -C " + compDir + " checkout --detach " + studioSHA,
		"/usr/bin/git -C " + compDir + " rev-parse HEAD",
		"/usr/bin/git -C " + compDir + " rev-parse --verify " + studioSHA + "^{commit}",
	})
}

func TestProvisionNoComponents(t *testing.T) {
	dir := t.TempDir()
	runner := &packagemanager.MockRunner{Strict: true}
	c := &Cloner{Runner: runner}

	if err := c.Provision(context.Background(), dir, nil); err != nil {
		t.Fatalf("Provision(nil): %v", err)
	}
	if err := c.Provision(context.Background(), dir, map[string]Component{}); err != nil {
		t.Fatalf("Provision(empty): %v", err)
	}
	wantCommands(t, runner, nil)
}

func TestProvisionEmptySrcDir(t *testing.T) {
	runner := &packagemanager.MockRunner{Strict: true}
	c := &Cloner{Runner: runner}
	if err := c.Provision(context.Background(), "", map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	}); err == nil {
		t.Fatalf("Provision: expected error for empty srcDir")
	}
	wantCommands(t, runner, nil)
}

func TestProvisionCloneError(t *testing.T) {
	dir := t.TempDir()
	sentinel := errors.New("clone failed")
	runner := &packagemanager.MockRunner{Strict: true, Responses: []packagemanager.CommandResponse{
		{Output: []byte("fatal: repository not found"), Err: sentinel},
	}}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Provision err = %v, want wrap of sentinel", err)
	}
	// No fetch/checkout after clone failure.
	compDir := filepath.Join(dir, "tt-studio")
	wantCommands(t, runner, []string{"git clone --filter=blob:none -- " + studioURL + " " + compDir})
}

func TestProvisionInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]struct {
		name string
		comp Component
	}{
		"empty name":      {"", Component{studioURL, studioSHA}},
		"traversal name":  {"..", Component{studioURL, studioSHA}},
		"separator name":  {"a/b", Component{studioURL, studioSHA}},
		"option name":     {"-x", Component{studioURL, studioSHA}},
		"empty url":       {"tt-studio", Component{"", studioSHA}},
		"option url":      {"tt-studio", Component{"--upload-pack=x", studioSHA}},
		"space url":       {"tt-studio", Component{"https://x y", studioSHA}},
		"empty version":   {"tt-studio", Component{studioURL, ""}},
		"option version":  {"tt-studio", Component{studioURL, "-x"}},
		"control version": {"tt-studio", Component{studioURL, "v1\n"}},
		"space name":      {"bad name", Component{studioURL, studioSHA}},
		"newline name":    {"bad\nname", Component{studioURL, studioSHA}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &packagemanager.MockRunner{Strict: true}
			c := &Cloner{Runner: runner}
			if err := c.Provision(context.Background(), dir, map[string]Component{tc.name: tc.comp}); err == nil {
				t.Fatalf("Provision: expected error")
			}
			wantCommands(t, runner, nil)
		})
	}
}

// TestProvisionInvalidEntryNoSideEffects ensures a manifest containing a valid
// and an invalid component fails before cloning the valid one or creating
// srcDir, so malformed input never produces partial provisioning.
func TestProvisionInvalidEntryNoSideEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src")
	runner := &packagemanager.MockRunner{Strict: true}
	c := &Cloner{Runner: runner}

	err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
		"bad":       {URL: "", Version: studioSHA},
	})
	if err == nil {
		t.Fatalf("Provision: expected error for invalid entry")
	}
	wantCommands(t, runner, nil)
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("srcDir should not be created when validation fails, stat err = %v", statErr)
	}
}

func TestProvisionRejectsSymlinkComponentDir(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(dir, "tt-studio")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	runner := &packagemanager.MockRunner{Strict: true}
	c := &Cloner{Runner: runner}

	if err := c.Provision(context.Background(), dir, map[string]Component{
		"tt-studio": {URL: studioURL, Version: studioSHA},
	}); err == nil {
		t.Fatalf("Provision: expected error for symlinked component dir")
	}
	wantCommands(t, runner, nil)
}
