package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/lock"
	"github.com/tetsuh/tt-env-go/pkg/manifest"
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
		if name == "apt-cache" {
			return []byte("9.9.9 1.0.0 2.0.0 3.0.0 4.0.0 5.0.0 1.1.0 1.2.0 1.3.0 1.4.0 1.5.0"), nil
		}
		if isPipVersionPreflightCommand(name, args) {
			return []byte("available"), nil
		}
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
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}})

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
	var logs []string
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})

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
	if !strings.Contains(strings.Join(logs, "\n"), "pinned system") || !strings.Contains(strings.Join(logs, "\n"), "container tt-metalium-ubuntu24") {
		t.Errorf("dry-run resolution summary missing pins or container identity: %v", logs)
	}
}

func TestInstallAlreadyInstalledIsNoOp(t *testing.T) {
	root, osRelease := setupRoot(t)
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})
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
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})
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

func TestInstallUnpinnedPackagesResolveAndLog(t *testing.T) {
	root, osRelease := setupRoot(t)
	unpinned := strings.ReplaceAll(testStackManifest, `"kmd": "1.0.0",
    "smi": "2.0.0",
    "flash": "3.0.0",
    "topology": "4.0.0",
    "metalium": "5.0.0"`, "")
	unpinned = strings.ReplaceAll(unpinned, `"tt-smi": "1.1.0",
    "tt-umd": "1.2.0",
    "textual": "1.3.0",
    "elasticsearch": "1.4.0",
    "tt-burnin": "1.5.0"`, "")
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"), unpinned)
	runner := cloneAwareRunner()
	var logs []string
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	l, err := lock.Read(filepath.Join(root, "versions", testRelease))
	if err != nil {
		t.Fatalf("lock.Read: %v", err)
	}
	if l.SystemPackages["kmd"] != "9.9.9" || l.PythonPackages["tt-smi"] != "9.9.9" {
		t.Fatalf("unpinned versions not resolved into lock: system=%v python=%v", l.SystemPackages, l.PythonPackages)
	}
	joined := strings.Join(logs, "\n")
	for _, want := range []string{"unpinned system candidates", "unpinned pip candidates", "optional metalium: skipped", "versions/" + testRelease + "/manifest.json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("summary missing %q: %s", want, joined)
		}
	}
}

func TestInstallForceReusesLockAndRejectsBadLock(t *testing.T) {
	root, osRelease := setupRoot(t)
	var logs []string
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	changed := strings.Replace(testStackManifest, `"kmd": "1.0.0"`, `"kmd": "99.0.0"`, 1)
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"), changed)
	runner := cloneAwareRunner()
	orch.Runner = runner
	if _, err := orch.Install(context.Background(), testRelease, Options{Force: true}); err != nil {
		t.Fatalf("force install: %v", err)
	}
	if got := installSpecs(runner); !contains(got, "tenstorrent-dkms=1.0.0") {
		t.Fatalf("force reinstall did not use lock version: %v", got)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "package resolution: installed lock replay") {
		t.Fatalf("force reinstall summary did not identify lock replay: %v", logs)
	}
	mustWrite(t, filepath.Join(root, "versions", testRelease, lock.FileName), "not json")
	if _, err := orch.Install(context.Background(), testRelease, Options{Force: true}); err == nil {
		t.Fatal("force reinstall must reject invalid lock")
	}
}

func TestForceWithoutLockFallsBackToCurrentManifest(t *testing.T) {
	root, osRelease := setupRoot(t)
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	if err := os.Remove(lock.Path(filepath.Join(root, "versions", testRelease))); err != nil {
		t.Fatal(err)
	}
	runner := cloneAwareRunner()
	orch.Runner = runner
	if _, err := orch.Install(context.Background(), testRelease, Options{Force: true}); err != nil {
		t.Fatalf("force install without legacy lock: %v", err)
	}
	if got := installSpecs(runner); !contains(got, "tenstorrent-dkms=1.0.0") {
		t.Fatalf("legacy force reinstall did not use manifest pins: %v", got)
	}
}

func installedLockFixture(t *testing.T) (root, releaseDir string, l *lock.Lock, orch *Orchestrator) {
	t.Helper()
	root, osRelease := setupRoot(t)
	orch = withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	releaseDir = filepath.Join(root, "versions", testRelease)
	var err error
	l, err = lock.Read(releaseDir)
	if err != nil {
		t.Fatal(err)
	}
	return root, releaseDir, l, orch
}

func TestForceRejectsIncompleteLockBeforeMutation(t *testing.T) {
	root, releaseDir, l, orch := installedLockFixture(t)
	delete(l.SystemPackages, "cmake")
	if err := lock.Write(releaseDir, l); err != nil {
		t.Fatal(err)
	}
	runner := cloneAwareRunner()
	orch.Runner = runner
	if _, err := orch.Install(context.Background(), testRelease, Options{Force: true}); err == nil || !strings.Contains(err.Error(), "no concrete version") || !strings.Contains(err.Error(), `"cmake"`) {
		t.Fatalf("expected incomplete lock error to name concrete package cmake, got %v", err)
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("incomplete lock must fail before commands, got %v", runner.Commands)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "."+testRelease+".partial")); !os.IsNotExist(err) {
		t.Fatalf("incomplete lock must fail before staging, stat error = %v", err)
	}
}

func TestForceRejectsLockForDifferentRelease(t *testing.T) {
	_, releaseDir, l, orch := installedLockFixture(t)
	l.Release = "2026.05.17"
	if err := lock.Write(releaseDir, l); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.Install(context.Background(), testRelease, Options{Force: true}); err == nil || !strings.Contains(err.Error(), "does not match requested release") {
		t.Fatalf("expected lock release identity error, got %v", err)
	}
}

func TestValidateLockedPackagesFallsBackToConcreteName(t *testing.T) {
	p := &plan{
		release:        testRelease,
		osm:            &manifest.OSManifest{},
		systemPackages: []packagemanager.Package{{Name: "unmapped-package"}},
	}
	if err := validateLockedPackages(p); err == nil || !strings.Contains(err.Error(), `"unmapped-package"`) {
		t.Fatalf("expected missing-version error to name concrete package, got %v", err)
	}
}

func TestPipPreflightComparesIndexedVersions(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	checkPackaging := exec.Command(python, "-c", "from pip._vendor.packaging.version import Version")
	if output, err := checkPackaging.CombinedOutput(); err != nil {
		t.Skipf("python3 pip vendored packaging is unavailable: %v: %s", err, output)
	}

	const mockedChecker = `
import subprocess
import sys
source, indexed_versions, package, pin = sys.argv[1:]
class Result:
    returncode = 0
    stdout = "Available versions: " + indexed_versions
    stderr = ""
def mock_run(args, **kwargs):
    expected = [sys.executable, "-m", "pip", "--no-cache-dir", "index", "versions", "--pre", package]
    if args != expected:
        raise AssertionError("unexpected pip index command: %r" % (args,))
    return Result()
subprocess.run = mock_run
sys.argv = ["pip-version-preflight", package, pin]
exec(source, {})
`
	for _, tc := range []struct {
		name            string
		indexedVersions string
		pin             string
		wantAvailable   bool
	}{
		{name: "prerelease accepted", indexedVersions: "1.0, 2.0rc1", pin: "2.0rc1", wantAvailable: true},
		{name: "PEP 440 equivalent accepted", indexedVersions: "1.0", pin: "1.0.0", wantAvailable: true},
		{name: "unavailable rejected", indexedVersions: "1.0", pin: "2.0", wantAvailable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(python, "-c", mockedChecker, pipVersionPreflightScript, tc.indexedVersions, "sample", tc.pin)
			output, err := cmd.CombinedOutput()
			if tc.wantAvailable && err != nil {
				t.Fatalf("checker rejected indexed version: %v: %s", err, output)
			}
			if !tc.wantAvailable && err == nil {
				t.Fatalf("checker accepted unavailable pin; output: %s", output)
			}
			if tc.wantAvailable && strings.TrimSpace(string(output)) != "available" {
				t.Fatalf("checker output = %q, want available", output)
			}
		})
	}
}

func TestDnfPreflightUsesLockedRpmVersionReleaseAndCache(t *testing.T) {
	runner := &packagemanager.MockRunner{RunFunc: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("1.0-2.fc40"), nil
	}}
	orch := &Orchestrator{Runner: runner}
	p := &plan{pkgManager: "dnf", systemPackages: []packagemanager.Package{{Name: "sample", Version: "1.0-2.fc40"}}}
	if err := orch.preflightPinnedPackages(context.Background(), p); err != nil {
		t.Fatalf("preflightPinnedPackages: %v", err)
	}
	if len(runner.Commands) != 1 {
		t.Fatalf("commands = %v, want one repoquery", runner.Commands)
	}
	cmd := runner.Commands[0]
	if cmd.Name != "dnf" || !hasPrefix(cmd.Args, []string{"--cacheonly", "repoquery", "--available", "--qf", "%{VERSION}-%{RELEASE}", "sample"}) {
		t.Fatalf("repoquery does not match installed RPM version-release: %+v", cmd)
	}
}

func TestPinnedUnavailableFailsBeforeMutation(t *testing.T) {
	root, osRelease := setupRoot(t)
	runner := cloneAwareRunner()
	runner.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "apt-cache" {
			return []byte(""), nil
		}
		return nil, nil
	}
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}})
	if _, err := orch.Install(context.Background(), testRelease, Options{}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected unavailable pin error, got %v", err)
	}
	for _, c := range runner.Commands {
		if c.Name == "sudo" {
			t.Fatalf("pinned preflight mutated system before failure: %v", runner.Commands)
		}
	}
}

func TestInstallRejectsMismatchedRelease(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"),
		strings.Replace(testStackManifest, `"release": "2026.05.16"`, `"release": "9999.01.01"`, 1))
	orch := withProbes(&Orchestrator{Root: root, Logf: func(string, ...any) {}})
	if _, err := orch.Install(context.Background(), testRelease, Options{DryRun: true}); err == nil {
		t.Fatal("expected error for mismatched release name")
	}
}

func TestInstallInvalidReleaseName(t *testing.T) {
	orch := withProbes(&Orchestrator{Root: t.TempDir()})
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

func isPipVersionPreflightCommand(name string, args []string) bool {
	return name == "python3" && len(args) == 4 && args[0] == "-c" && args[1] == pipVersionPreflightScript && args[2] != "" && args[3] != ""
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
		if name == "apt-cache" {
			return []byte("9.9.9 1.0.0 2.0.0 3.0.0 4.0.0 5.0.0 1.1.0 1.2.0 1.3.0 1.4.0 1.5.0"), nil
		}
		if isPipVersionPreflightCommand(name, args) {
			return []byte("available"), nil
		}
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
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}})

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
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}})

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
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}})

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
	orch := withProbes(&Orchestrator{Root: root, Runner: latestAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})

	if _, err := orch.Install(context.Background(), "2026.08.01", Options{Latest: true, Base: "2026.07.01"}); err == nil {
		t.Fatal("expected error for mismatched base manifest release")
	}
}

func TestInstallLatestOmitsUndeclaredOptionalPackage(t *testing.T) {
	root, osRelease := setupRoot(t)
	// Base manifest omits the optional "metalium" system package.
	noMetalium := strings.Replace(testStackManifest, `,
    "metalium": "5.0.0"`, ``, 1)
	if noMetalium == testStackManifest {
		t.Fatal("test fixture not modified; metalium line not found")
	}
	mustWrite(t, filepath.Join(root, "releases", "2026.09.01.json"),
		strings.Replace(noMetalium, `"release": "2026.05.16"`, `"release": "2026.09.01"`, 1))
	runner := latestAwareRunner()
	orch := withProbes(&Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}})

	if _, err := orch.Install(context.Background(), "2026.09.02", Options{Latest: true, Base: "2026.09.01"}); err != nil {
		t.Fatalf("Install --latest: %v", err)
	}
	specs := installSpecs(runner)
	for _, s := range specs {
		if s == "tt-metalium" || strings.HasPrefix(s, "tt-metalium=") {
			t.Errorf("optional package omitted in base must not be installed in --latest mode; specs: %v", specs)
		}
	}
	// Required build packages are still installed unpinned.
	if !contains(specs, "cmake") {
		t.Errorf("expected required package cmake in specs: %v", specs)
	}
}

// withProbes injects fake installed-version probes so lock resolution succeeds
// without a real dpkg/pip; every probed package reports version 9.9.9.
func withProbes(orch *Orchestrator) *Orchestrator {
	orch.DpkgVersion = func(ctx context.Context, name string) (string, bool, error) {
		return "9.9.9", true, nil
	}
	orch.PipShowVersion = func(ctx context.Context, venvPython, pkg string) (string, bool, error) {
		return "9.9.9", true, nil
	}
	return orch
}

func TestInstallWritesLockPinned(t *testing.T) {
	root, osRelease := setupRoot(t)
	// Record the catalog provenance the way a real "tt-env update" does.
	mustWrite(t, filepath.Join(root, "manifests", "catalog_source.json"),
		`{"repo":"tetsuh/tt-env-manifests","ref":"main","updated_at":"2026-09-22T00:00:00Z"}`)
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})

	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	l, err := lock.Read(filepath.Join(root, "versions", testRelease))
	if err != nil {
		t.Fatalf("lock.Read: %v", err)
	}
	// Pinned entries record their pins: the exact versions installed.
	for virtual, want := range map[string]string{"kmd": "1.0.0", "smi": "2.0.0", "flash": "3.0.0", "topology": "4.0.0", "metalium": "5.0.0"} {
		if got := l.SystemPackages[virtual]; got != want {
			t.Errorf("lock system package %s = %q, want %q", virtual, got, want)
		}
	}
	if got := l.PythonPackages["tt-smi"]; got != "1.1.0" {
		t.Errorf("lock python package tt-smi = %q, want 1.1.0", got)
	}
	if gc := l.GitComponents["tt-foo"]; gc.Version != "v1.0.0" {
		t.Errorf("lock git component tt-foo = %+v, want v1.0.0", gc)
	}
	if l.Source != lock.SourceCatalog {
		t.Errorf("lock source = %q, want catalog", l.Source)
	}
	if l.CatalogRepo != "tetsuh/tt-env-manifests" || l.CatalogRef != "main" {
		t.Errorf("lock catalog provenance = %q@%q", l.CatalogRepo, l.CatalogRef)
	}
	if l.InstalledAt.IsZero() {
		t.Error("lock installed_at is zero")
	}
}

func TestInstallWritesLockLatest(t *testing.T) {
	root, osRelease := setupRoot(t)
	orch := withProbes(&Orchestrator{Root: root, Runner: latestAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})

	if _, err := orch.Install(context.Background(), "2026.06.01", Options{Latest: true, Base: testRelease}); err != nil {
		t.Fatalf("Install --latest --base: %v", err)
	}

	l, err := lock.Read(filepath.Join(root, "versions", "2026.06.01"))
	if err != nil {
		t.Fatalf("lock.Read: %v", err)
	}
	// Unpinned --latest entries record the probed installed versions.
	for virtual := range l.SystemPackages {
		if got := l.SystemPackages[virtual]; got != "9.9.9" {
			t.Errorf("lock system package %s = %q, want probed 9.9.9", virtual, got)
		}
	}
	for pkg := range l.PythonPackages {
		if got := l.PythonPackages[pkg]; got != "9.9.9" {
			t.Errorf("lock python package %s = %q, want probed 9.9.9", pkg, got)
		}
	}
	// Git components are pinned to their resolved remote HEAD.
	if gc := l.GitComponents["tt-foo"]; gc.Version != latestHeadSHA {
		t.Errorf("lock git component tt-foo = %+v, want %s", gc, latestHeadSHA)
	}
	if l.Source != lock.SourceLatest {
		t.Errorf("lock source = %q, want latest", l.Source)
	}
	if l.Base != testRelease {
		t.Errorf("lock base = %q, want %q", l.Base, testRelease)
	}
	if l.Release != "2026.06.01" {
		t.Errorf("lock release = %q, want the installed release name", l.Release)
	}
}

func TestInstallWritesLockLocalSource(t *testing.T) {
	root, osRelease := setupRoot(t)
	// Move the plan manifest into releases.local/ so the lock cites a local
	// manifest as its source.
	mustWrite(t, filepath.Join(root, "releases.local", testRelease+".json"), testStackManifest)
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})

	if _, err := orch.Install(context.Background(), testRelease, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	l, err := lock.Read(filepath.Join(root, "versions", testRelease))
	if err != nil {
		t.Fatalf("lock.Read: %v", err)
	}
	if l.Source != lock.SourceLocal {
		t.Errorf("lock source = %q, want local", l.Source)
	}
	if l.CatalogRepo != "" {
		t.Errorf("local plan must not cite a catalog: %q", l.CatalogRepo)
	}
}

func TestSystemPackageVersionUsesManager(t *testing.T) {
	calls := 0
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "rpm" || strings.Join(args, " ") != "-q --qf %{VERSION}-%{RELEASE} -- cmake" {
			t.Fatalf("dnf version query = %s %v", name, args)
		}
		return []byte("3.4-5.fc42\n"), nil
	}
	orch := &Orchestrator{
		Runner: runner,
		DpkgVersion: func(context.Context, string) (string, bool, error) {
			calls++
			return "apt-version", true, nil
		},
	}

	version, installed, err := orch.systemPackageVersion(context.Background(), "apt", "cmake")
	if err != nil || !installed || version != "apt-version" || calls != 1 {
		t.Fatalf("apt systemPackageVersion() = %q, %v, %v (dpkg calls=%d)", version, installed, err, calls)
	}
	version, installed, err = orch.systemPackageVersion(context.Background(), "dnf", "cmake")
	if err != nil || !installed || version != "3.4-5.fc42" || calls != 1 {
		t.Fatalf("dnf systemPackageVersion() = %q, %v, %v (dpkg calls=%d)", version, installed, err, calls)
	}
}

func TestInstallLockProbesReportNotInstalled(t *testing.T) {
	root, osRelease := setupRoot(t)
	orch := withProbes(&Orchestrator{Root: root, Runner: cloneAwareRunner(), OSReleasePath: osRelease, Logf: func(string, ...any) {}})
	// The unpinned cmake package reports as not installed: the lock must
	// refuse to record a phantom version.
	orch.DpkgVersion = func(ctx context.Context, name string) (string, bool, error) {
		return "", false, nil
	}

	if _, err := orch.Install(context.Background(), testRelease, Options{}); err == nil || !strings.Contains(err.Error(), "cmake") {
		t.Fatalf("Install error = %v, want cmake not installed", err)
	}
}
