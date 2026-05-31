package shims

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fakeBin creates an executable file at <home>/versions/<release>/bin/<command>
// whose body is the given shell snippet.
func fakeBin(t *testing.T, home, release, command, body string) {
	t.Helper()
	dir := filepath.Join(home, "versions", release, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := "#!/usr/bin/env bash\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, command), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}
}

// pointCurrent atomically points <home>/current at <home>/versions/<release>.
func pointCurrent(t *testing.T, home, release string) {
	t.Helper()
	target := filepath.Join(home, "versions", release)
	link := filepath.Join(home, "current")
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		t.Fatalf("rename current: %v", err)
	}
}

func TestKnownCommandsSortedAndComplete(t *testing.T) {
	got := KnownCommands()
	if !sort.StringsAreSorted(got) {
		t.Errorf("KnownCommands not sorted: %v", got)
	}
	want := map[string]bool{
		"tt-smi": true, "tt-flash": true, "tt-topology": true, "tt-burnin": true,
		"tt-metalium": true, "tt-metalium-ubuntu22": true, "tt-metalium-ubuntu24": true,
		"tt-studio": true, "tt-inference-server": true, "tt-metalium-models": true,
	}
	if len(got) != len(want) {
		t.Fatalf("KnownCommands = %v (len %d), want %d", got, len(got), len(want))
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("unexpected command %q", c)
		}
	}
}

func TestIsOptional(t *testing.T) {
	if IsOptional("tt-smi") {
		t.Errorf("tt-smi should be required")
	}
	if !IsOptional("tt-studio") {
		t.Errorf("tt-studio should be optional")
	}
}

func TestGenerateWritesExecutableShims(t *testing.T) {
	home := t.TempDir()
	g := &Generator{Home: home}

	written, err := g.Generate("tt-smi", "tt-flash")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := []string{
		filepath.Join(g.ShimsDir(), "tt-flash"),
		filepath.Join(g.ShimsDir(), "tt-smi"),
	}
	if !reflect.DeepEqual(written, want) {
		t.Fatalf("written = %v, want %v", written, want)
	}

	for _, p := range want {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable: %v", p, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		// The shim resolves through current at run time, with no baked-in path.
		if !strings.Contains(string(data), `${TT_HOME}/current/bin/${command_name}`) {
			t.Errorf("%s does not resolve through current/bin: %s", p, data)
		}
		if strings.Contains(string(data), home) {
			t.Errorf("%s bakes in an absolute home path: %s", p, data)
		}
	}
}

func TestGenerateDefaultsToKnownCommands(t *testing.T) {
	home := t.TempDir()
	g := &Generator{Home: home}

	written, err := g.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(written) != len(KnownCommands()) {
		t.Errorf("wrote %d shims, want %d", len(written), len(KnownCommands()))
	}
}

func TestGenerateInvalidInputs(t *testing.T) {
	home := t.TempDir()
	g := &Generator{Home: home}
	for _, name := range []string{"", ".", "..", "a/b", "-x", "bad name", "bad\nname"} {
		if _, err := g.Generate(name); err == nil {
			t.Errorf("Generate(%q): expected error", name)
		}
	}
	if _, err := (&Generator{}).Generate("tt-smi"); err == nil {
		t.Errorf("Generate with empty Home: expected error")
	}
}

func TestResolveFollowsActiveRelease(t *testing.T) {
	home := t.TempDir()
	g := &Generator{Home: home}
	fakeBin(t, home, "r1", "tt-smi", "echo r1")
	fakeBin(t, home, "r2", "tt-smi", "echo r2")

	pointCurrent(t, home, "r1")
	got, err := g.Resolve("tt-smi")
	if err != nil {
		t.Fatalf("Resolve r1: %v", err)
	}
	if want := filepath.Join(home, "current", "bin", "tt-smi"); got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}

	// Switching the active release changes resolution with no regeneration.
	pointCurrent(t, home, "r2")
	if _, err := g.Resolve("tt-smi"); err != nil {
		t.Fatalf("Resolve r2: %v", err)
	}
}

func TestResolveMissingOrNotExecutable(t *testing.T) {
	home := t.TempDir()
	g := &Generator{Home: home}

	// No current symlink at all.
	if _, err := g.Resolve("tt-smi"); err == nil {
		t.Errorf("Resolve with no release: expected error")
	}

	// Present but not executable.
	dir := filepath.Join(home, "versions", "r1", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tt-smi"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pointCurrent(t, home, "r1")
	if _, err := g.Resolve("tt-smi"); err == nil {
		t.Errorf("Resolve non-executable: expected error")
	}

	if _, err := g.Resolve("../evil"); err == nil {
		t.Errorf("Resolve invalid name: expected error")
	}
}

// TestGeneratedShimDispatches runs a generated shim through bash to prove it
// dispatches to the active release's binary and follows release switches.
func TestGeneratedShimDispatches(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	home := t.TempDir()
	g := &Generator{Home: home}
	fakeBin(t, home, "r1", "tt-smi", `echo "from r1: $*"`)
	fakeBin(t, home, "r2", "tt-smi", `echo "from r2: $*"`)
	pointCurrent(t, home, "r1")

	if _, err := g.Generate("tt-smi"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	shim := filepath.Join(g.ShimsDir(), "tt-smi")

	run := func() string {
		t.Helper()
		cmd := exec.Command(bash, shim, "arg1")
		cmd.Env = append(os.Environ(), "TT_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run shim: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}

	if got := run(); got != "from r1: arg1" {
		t.Errorf("shim output = %q, want %q", got, "from r1: arg1")
	}

	// Switch the active release; the same shim now dispatches elsewhere.
	pointCurrent(t, home, "r2")
	if got := run(); got != "from r2: arg1" {
		t.Errorf("shim output after switch = %q, want %q", got, "from r2: arg1")
	}
}

// TestGeneratedShimFailsClearlyWhenMissing verifies the shim errors when the
// active release lacks the command.
func TestGeneratedShimFailsClearlyWhenMissing(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	home := t.TempDir()
	g := &Generator{Home: home}
	// Release exists but has no tt-smi binary.
	if err := os.MkdirAll(filepath.Join(home, "versions", "r1", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pointCurrent(t, home, "r1")
	if _, err := g.Generate("tt-smi"); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	cmd := exec.Command(bash, filepath.Join(g.ShimsDir(), "tt-smi"))
	cmd.Env = append(os.Environ(), "TT_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected shim to fail, output: %s", out)
	}
	if !strings.Contains(string(out), "not found or not executable") {
		t.Errorf("unexpected error output: %s", out)
	}
}
