package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

const testRelease = "2026.05.16"

const testStackManifest = `{
  "release": "2026.05.16",
  "description": "test release",
  "system_packages": {
    "kmd": "1.0.0",
    "smi": "2.0.0",
    "flash": "3.0.0",
    "topology": "4.0.0",
    "metalium": "5.0.0"
  },
  "python_packages": {
    "tt-smi": "1.1.0",
    "tt-umd": "1.2.0",
    "textual": "1.3.0",
    "elasticsearch": "1.4.0",
    "tt-burnin": "1.5.0"
  },
  "git_components": {
    "tt-foo": {"url": "https://github.com/tenstorrent/tt-foo.git", "version": "v1.0.0"}
  },
  "container_components": {
    "tt-metalium-ubuntu24": {"image_url": "ghcr.io/tenstorrent/tt-metalium", "image_tag": "sha256:abc123"},
    "tt-metalium": {"ref": "tt-metalium-ubuntu24"}
  }
}`

const testOSManifest = `PKG_MANAGER="apt"
USE_SYSTEM_PACKAGES="true"
REQUIRED_REPOS=(
  "https://ppa.tenstorrent.com/ubuntu/"
)
VIRT_PKG_CMAKE="cmake"
VIRT_PKG_NINJA="ninja-build"
VIRT_PKG_ZLIB="zlib1g-dev"
VIRT_PKG_KMD="tenstorrent-dkms"
VIRT_PKG_SMI="tt-smi"
VIRT_PKG_FLASH="tt-flash"
VIRT_PKG_TOPOLOGY="tt-topology"
VIRT_PKG_METALIUM="tt-metalium"
`

const testOSRelease = `ID=ubuntu
VERSION_ID="24.04"
VERSION_CODENAME=noble
`

// setupRoot creates a temporary TT_HOME with the stack and OS manifests and an
// os-release file, returning the root and os-release path.
func setupRoot(t *testing.T) (root, osReleasePath string) {
	t.Helper()
	root = t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"), testStackManifest)
	mustWrite(t, filepath.Join(root, "manifests", "ubuntu-24.04.env"), testOSManifest)
	osReleasePath = filepath.Join(root, "os-release")
	mustWrite(t, osReleasePath, testOSRelease)
	return root, osReleasePath
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// cloneAwareRunner returns a MockRunner whose RunFunc materializes a git
// component's entrypoint when it observes a `git clone`, so the orchestrator's
// entrypoint check passes.
func cloneAwareRunner() *packagemanager.MockRunner {
	r := &packagemanager.MockRunner{}
	r.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "git" && len(args) > 0 && args[0] == "clone" {
			dest := args[len(args)-1]
			_ = os.MkdirAll(dest, 0o755)
			_ = os.WriteFile(filepath.Join(dest, "run.py"), []byte("#!/usr/bin/env python\n"), 0o755)
		}
		if name == "git" && containsArg(args, "rev-parse") {
			return []byte("0123456789abcdef0123456789abcdef01234567\n"), nil
		}
		return nil, nil
	}
	return r
}

func TestInstallSystemPackagePath(t *testing.T) {
	root, osRelease := setupRoot(t)
	runner := cloneAwareRunner()
	orch := &Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}}

	res, err := orch.Install(context.Background(), testRelease, Options{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Installed {
		t.Fatalf("expected Installed=true, got %+v", res)
	}

	versionDir := filepath.Join(root, "versions", testRelease)
	for _, rel := range []string{
		".tt-env-installed",
		filepath.Join("bin", "tt-foo"),
		filepath.Join("bin", "tt-metalium-ubuntu24"),
	} {
		if _, err := os.Stat(filepath.Join(versionDir, rel)); err != nil {
			t.Errorf("expected %s in version dir: %v", rel, err)
		}
	}

	// Ref-only container component must NOT get a wrapper.
	if _, err := os.Stat(filepath.Join(versionDir, "bin", "tt-metalium")); !os.IsNotExist(err) {
		t.Errorf("ref-only container component tt-metalium should not have a wrapper")
	}

	// Shims are generated under root/shims.
	if _, err := os.Stat(filepath.Join(root, "shims", "tt-smi")); err != nil {
		t.Errorf("expected shim tt-smi: %v", err)
	}

	assertCommandSeen(t, runner, "sudo", "add-apt-repository")
	assertCommandSeen(t, runner, "sudo", "apt-get", "update")
	assertCommandSeen(t, runner, "sudo", "apt-get", "install")
	assertCommandSeen(t, runner, "git", "clone")

	// System packages installed with expected pins.
	specs := installSpecs(runner)
	for _, want := range []string{
		"cmake", "ninja-build", "zlib1g-dev",
		"tenstorrent-dkms=1.0.0", "tt-smi=2.0.0", "tt-flash=3.0.0",
		"tt-topology=4.0.0", "tt-metalium=5.0.0",
	} {
		if !contains(specs, want) {
			t.Errorf("apt install missing spec %q (got %v)", want, specs)
		}
	}
}

func TestInstallDryRunDoesNotStage(t *testing.T) {
	root, osRelease := setupRoot(t)
	runner := cloneAwareRunner()
	orch := &Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}}

	res, err := orch.Install(context.Background(), testRelease, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	if res.Installed {
		t.Errorf("dry-run must not report Installed")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("dry-run must not run any commands, got %d", len(runner.Commands))
	}
	if _, err := os.Stat(filepath.Join(root, "versions", testRelease)); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create the version dir")
	}
}

func TestInstallAlreadyInstalledIsNoOp(t *testing.T) {
	root, osRelease := setupRoot(t)
	orch := &Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}}
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Second install with a fresh runner: stage must not be called, so no
	// commands run, but shims are still regenerated.
	runner := cloneAwareRunner()
	orch.Runner = runner
	res, err := orch.Install(context.Background(), testRelease, Options{})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Installed {
		t.Errorf("expected no-op (Installed=false) on reinstall, got %+v", res)
	}
	if len(runner.Commands) != 0 {
		t.Errorf("already-installed no-op must not run commands, got %v", runner.Commands)
	}
}

func TestInstallForceReinstalls(t *testing.T) {
	root, osRelease := setupRoot(t)
	orch := &Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}}
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	runner := cloneAwareRunner()
	orch.Runner = runner
	res, err := orch.Install(context.Background(), testRelease, Options{Force: true})
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if !res.Installed || !res.Replaced {
		t.Errorf("expected force reinstall Installed && Replaced, got %+v", res)
	}
	if len(runner.Commands) == 0 {
		t.Errorf("force reinstall must run staging commands")
	}
}

func TestInstallRejectsMismatchedRelease(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"),
		strings.Replace(testStackManifest, `"release": "2026.05.16"`, `"release": "9999.01.01"`, 1))
	orch := &Orchestrator{Root: root, Logf: func(string, ...any) {}}
	if _, err := orch.Install(context.Background(), testRelease, Options{DryRun: true}); err == nil {
		t.Fatal("expected error for mismatched release name")
	}
}

func TestInstallInvalidReleaseName(t *testing.T) {
	orch := &Orchestrator{Root: t.TempDir()}
	if _, err := orch.Install(context.Background(), "../escape", Options{}); err == nil {
		t.Fatal("expected error for invalid release name")
	}
}

// --- helpers ---

func assertCommandSeen(t *testing.T, r *packagemanager.MockRunner, name string, argPrefix ...string) {
	t.Helper()
	for _, c := range r.Commands {
		if c.Name != name {
			continue
		}
		if hasPrefix(c.Args, argPrefix) {
			return
		}
	}
	t.Errorf("expected command %s %v to be run; recorded: %v", name, argPrefix, r.Commands)
}

func hasPrefix(args, prefix []string) bool {
	if len(prefix) > len(args) {
		return false
	}
	for i, p := range prefix {
		if args[i] != p {
			return false
		}
	}
	return true
}

// installSpecs returns the package specs from the recorded apt-get install call.
func installSpecs(r *packagemanager.MockRunner) []string {
	for _, c := range r.Commands {
		if c.Name == "sudo" && len(c.Args) >= 2 && c.Args[0] == "apt-get" && c.Args[1] == "install" {
			var specs []string
			seenSep := false
			for _, a := range c.Args[2:] {
				if a == "--" {
					seenSep = true
					continue
				}
				if seenSep {
					specs = append(specs, a)
				}
			}
			return specs
		}
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

const latestHeadSHA = "fedcba9876543210fedcba9876543210fedcba98"

// latestAwareRunner extends cloneAwareRunner to answer `git ls-remote --symref`
// with a programmed HEAD SHA so the --latest path can resolve git components.
func latestAwareRunner() *packagemanager.MockRunner {
	r := &packagemanager.MockRunner{}
	r.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "git" && len(args) > 0 && args[0] == "ls-remote" {
			return []byte("ref: refs/heads/main\tHEAD\n" + latestHeadSHA + "\tHEAD\n"), nil
		}
		if name == "git" && len(args) > 0 && args[0] == "clone" {
			dest := args[len(args)-1]
			_ = os.MkdirAll(dest, 0o755)
			_ = os.WriteFile(filepath.Join(dest, "run.py"), []byte("#!/usr/bin/env python\n"), 0o755)
		}
		if name == "git" && containsArg(args, "rev-parse") {
			return []byte(latestHeadSHA + "\n"), nil
		}
		return nil, nil
	}
	return r
}

func TestInstallLatestUnpinned(t *testing.T) {
	root, osRelease := setupRoot(t)
	runner := latestAwareRunner()
	orch := &Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}}

	res, err := orch.Install(context.Background(), testRelease, Options{Latest: true})
	if err != nil {
		t.Fatalf("Install --latest: %v", err)
	}
	if !res.Installed {
		t.Fatalf("expected Installed=true, got %+v", res)
	}

	// System packages are installed unpinned (no "=version" suffix).
	specs := installSpecs(runner)
	for _, s := range specs {
		if strings.Contains(s, "=") {
			t.Errorf("latest install must not pin system package, got %q", s)
		}
	}
	for _, want := range []string{
		"cmake", "ninja-build", "zlib1g-dev", "tenstorrent-dkms",
		"tt-smi", "tt-flash", "tt-topology", "tt-metalium",
	} {
		if !contains(specs, want) {
			t.Errorf("apt install missing unpinned spec %q (got %v)", want, specs)
		}
	}

	// Pip packages are installed unpinned (no "==").
	pip := pipInstallSpecs(runner)
	if len(pip) == 0 {
		t.Fatalf("expected pip install command")
	}
	for _, s := range pip {
		if strings.Contains(s, "==") {
			t.Errorf("latest install must not pin pip package, got %q", s)
		}
	}

	// Git component is cloned at the resolved remote HEAD.
	assertCommandSeen(t, runner, "git", "ls-remote", "--symref")
	if !gitCheckoutSeen(runner, latestHeadSHA) {
		t.Errorf("expected git checkout at resolved HEAD %s; commands: %v", latestHeadSHA, runner.Commands)
	}
}

func TestInstallLatestRequiresForceWhenInstalled(t *testing.T) {
	root, osRelease := setupRoot(t)
	runner := latestAwareRunner()
	orch := &Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}}

	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	if _, err := orch.Install(context.Background(), testRelease, Options{Latest: true}); err == nil {
		t.Fatalf("expected error refreshing installed release without --force")
	}
	res, err := orch.Install(context.Background(), testRelease, Options{Latest: true, Force: true})
	if err != nil {
		t.Fatalf("Install --latest --force: %v", err)
	}
	if !res.Installed {
		t.Errorf("expected Installed=true on force refresh, got %+v", res)
	}
}

func TestInstallLatestUsesBaseManifest(t *testing.T) {
	root, osRelease := setupRoot(t)
	// Target release has no manifest of its own; --base supplies the structure.
	const target = "2026.06.01"
	runner := latestAwareRunner()
	orch := &Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}}

	res, err := orch.Install(context.Background(), target, Options{Latest: true, Base: testRelease})
	if err != nil {
		t.Fatalf("Install --latest --base: %v", err)
	}
	if !res.Installed {
		t.Fatalf("expected Installed=true, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", target, ".tt-env-installed")); err != nil {
		t.Errorf("expected target installed into versions/%s: %v", target, err)
	}
	// install --latest must not write a manifest for the target.
	if _, err := os.Stat(filepath.Join(root, "releases", target+".json")); !os.IsNotExist(err) {
		t.Errorf("install --latest must not write releases/%s.json", target)
	}
}

// pipInstallSpecs returns the package specs from the recorded pip install call.
func pipInstallSpecs(r *packagemanager.MockRunner) []string {
	for _, c := range r.Commands {
		if containsArg(c.Args, "pip") && containsArg(c.Args, "install") {
			var specs []string
			for _, a := range c.Args {
				if a == "-m" || a == "pip" || a == "install" || a == "--disable-pip-version-check" {
					continue
				}
				specs = append(specs, a)
			}
			return specs
		}
	}
	return nil
}

func gitCheckoutSeen(r *packagemanager.MockRunner, sha string) bool {
	for _, c := range r.Commands {
		if c.Name != "git" {
			continue
		}
		if containsArg(c.Args, "checkout") && containsArg(c.Args, sha) {
			return true
		}
	}
	return false
}

func TestInstallLatestRejectsMismatchedBaseManifest(t *testing.T) {
	root, osRelease := setupRoot(t)
	// Base manifest declares a different release than its filename.
	mustWrite(t, filepath.Join(root, "releases", "2026.07.01.json"),
		strings.Replace(testStackManifest, `"release": "2026.05.16"`, `"release": "9999.01.01"`, 1))
	orch := &Orchestrator{Root: root, Runner: latestAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}}

	if _, err := orch.Install(context.Background(), "2026.08.01", Options{Latest: true, Base: "2026.07.01"}); err == nil {
		t.Fatal("expected error for mismatched base manifest release")
	}
}
