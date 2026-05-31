package capture

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
)

const baseRelease = "2026.05.16"

const baseStackManifest = `{
  "release": "2026.05.16",
  "description": "base stack",
  "components": {
    "tt-kmd": "ttkmd-2.0.0",
    "tt-smi": "v1.0.0",
    "firmware": "v19.6.0",
    "tt-metal": "v0.70.1"
  },
  "system_packages": {
    "kmd": "2.0.0", "smi": "1.0.0", "flash": "1.0.0", "topology": "1.0.0", "metalium": "0.1.0"
  },
  "python_packages": {
    "tt-smi": "1.0.0", "tt-umd": "0.1.0", "textual": "0.1.0", "elasticsearch": "8.0.0", "tt-burnin": "0.1.0"
  },
  "git_components": {
    "tt-studio": {"url": "https://github.com/tenstorrent/tt-studio.git", "version": "1111111111111111111111111111111111111111"},
    "tt-inference-server": {"url": "https://github.com/tenstorrent/tt-inference-server.git", "version": "2222222222222222222222222222222222222222"}
  },
  "container_components": {
    "tt-metalium": {"ref": "tt-metalium-ubuntu24"},
    "tt-metalium-ubuntu24": {"image_url": "ghcr.io/tenstorrent/tt-metal/tt-metalium-ubuntu-24.04-release-amd64", "image_tag": "sha256:ead7b800bdb6bebb9425c377222314447c5b2052f6e8b1e3c9caa1818cb7d8c4"}
  }
}`

const captureOSManifest = `PKG_MANAGER="apt"
USE_SYSTEM_PACKAGES="true"
VIRT_PKG_CMAKE="cmake"
VIRT_PKG_NINJA="ninja-build"
VIRT_PKG_ZLIB="zlib1g-dev"
VIRT_PKG_KMD="tenstorrent-dkms"
VIRT_PKG_SMI="tt-smi"
VIRT_PKG_FLASH="tt-flash"
VIRT_PKG_TOPOLOGY="tt-topology"
VIRT_PKG_METALIUM="tt-metalium"
`

const captureOSRelease = `ID=ubuntu
VERSION_ID="24.04"
VERSION_CODENAME=noble
`

var installedDpkgVersions = map[string]string{
	"tenstorrent-dkms": "2.8.0",
	"tt-smi":           "5.0.1",
	"tt-flash":         "3.6.5",
	"tt-topology":      "1.2.19",
	"tt-metalium":      "0.69.0~ubuntu24.04",
}

var installedPipVersions = map[string]string{
	"tt-smi":        "5.2.0",
	"tt-umd":        "0.9.5",
	"textual":       "0.59.0",
	"elasticsearch": "8.11.0",
	"tt-burnin":     "0.4.0",
}

var installedGitHeads = map[string]string{
	"tt-studio":           "a6d347af3980540bb16d10ec473a6b09ce6f2138",
	"tt-inference-server": "cfa35731abe68484077d7b6337e7a11c4b2bdaa6",
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

// setupRoot creates a TT_HOME with an installed base release and returns the
// root and os-release path.
func setupRoot(t *testing.T) (root, osReleasePath string) {
	t.Helper()
	root = t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", baseRelease+".json"), baseStackManifest)
	mustWrite(t, filepath.Join(root, "manifests", "ubuntu-24.04.env"), captureOSManifest)
	osReleasePath = filepath.Join(root, "os-release")
	mustWrite(t, osReleasePath, captureOSRelease)

	// Mark the base release installed with a virtualenv python and git clones.
	versionDir := filepath.Join(root, "versions", baseRelease)
	mustWrite(t, filepath.Join(versionDir, ".tt-env-installed"), `{}`)
	mustWrite(t, filepath.Join(versionDir, "venv", "bin", "python"), "#!/bin/sh\n")
	for name := range installedGitHeads {
		mustWrite(t, filepath.Join(versionDir, "src", name, ".git", "HEAD"), "ref: refs/heads/main\n")
	}
	return root, osReleasePath
}

func newCapturer(t *testing.T, root, osRelease string) *Capturer {
	t.Helper()
	return &Capturer{
		Root:          root,
		OSReleasePath: osRelease,
		Logf:          func(string, ...any) {},
		DpkgVersion: func(_ context.Context, name string) (string, bool, error) {
			v, ok := installedDpkgVersions[name]
			return v, ok, nil
		},
		PipShowVersion: func(_ context.Context, _ /*venvPython*/, pkg string) (string, bool, error) {
			v, ok := installedPipVersions[pkg]
			return v, ok, nil
		},
		GitHead: func(_ context.Context, repoDir string) (string, error) {
			return installedGitHeads[filepath.Base(repoDir)], nil
		},
	}
}

func TestCaptureDryRunProbesInstalledVersions(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)

	res, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true})
	if err != nil {
		t.Fatalf("Capture dry-run: %v", err)
	}
	if res.Written {
		t.Error("dry-run must not write")
	}
	if res.BaseRelease != baseRelease {
		t.Errorf("base = %q, want %q", res.BaseRelease, baseRelease)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "2026.06.01.json")); !os.IsNotExist(err) {
		t.Error("dry-run must not create the target manifest")
	}

	var m manifest.Manifest
	if err := json.Unmarshal(res.ManifestJSON, &m); err != nil {
		t.Fatalf("rendered manifest is not valid JSON: %v", err)
	}
	if m.SystemPackages["kmd"] != "2.8.0" {
		t.Errorf("system_packages.kmd = %q, want 2.8.0", m.SystemPackages["kmd"])
	}
	if m.SystemPackages["metalium"] != "0.69.0~ubuntu24.04" {
		t.Errorf("system_packages.metalium = %q", m.SystemPackages["metalium"])
	}
	if m.PythonPackages["tt-smi"] != "5.2.0" {
		t.Errorf("python_packages.tt-smi = %q, want 5.2.0", m.PythonPackages["tt-smi"])
	}
	if m.GitComponents["tt-studio"].Version != installedGitHeads["tt-studio"] {
		t.Errorf("git tt-studio version = %q", m.GitComponents["tt-studio"].Version)
	}
	if m.Components["tt-kmd"].Version != "ttkmd-2.8.0" {
		t.Errorf("components.tt-kmd = %q, want ttkmd-2.8.0", m.Components["tt-kmd"].Version)
	}
	if m.Components["tt-smi"].Version != "v5.2.0" {
		t.Errorf("components.tt-smi = %q, want v5.2.0", m.Components["tt-smi"].Version)
	}
	// tt-metal and firmware are carried over from the base.
	if m.Components["tt-metal"].Version != "v0.70.1" {
		t.Errorf("components.tt-metal = %q, want v0.70.1 (from base)", m.Components["tt-metal"].Version)
	}
	// Container components are copied from the base unchanged.
	if m.ContainerComponents["tt-metalium-ubuntu24"].ImageTag == "" {
		t.Error("container components should be carried over from base")
	}
}

func TestCaptureWritesManifest(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)

	res, err := c.Capture(context.Background(), "2026.06.01", Options{})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !res.Written {
		t.Fatal("expected Written=true")
	}
	target := filepath.Join(root, "releases", "2026.06.01.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read written manifest: %v", err)
	}
	if _, err := manifest.Load(target); err != nil {
		t.Errorf("written manifest fails to load/validate: %v", err)
	}
	if !strings.Contains(string(data), `"2.8.0"`) {
		t.Errorf("written manifest missing probed kmd version:\n%s", data)
	}
}

func TestCaptureExistingTargetRequiresForce(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)
	mustWrite(t, filepath.Join(root, "releases", "2026.06.01.json"), `{"release":"x"}`)

	if _, err := c.Capture(context.Background(), "2026.06.01", Options{}); err == nil {
		t.Fatal("expected error when target exists without --force")
	}
	// --force overwrites.
	if _, err := c.Capture(context.Background(), "2026.06.01", Options{Force: true}); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
}

func TestCaptureRequiresInstalledBase(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)

	// An explicit base that has a manifest but no installed tree must fail.
	mustWrite(t, filepath.Join(root, "releases", "2026.01.01.json"), baseStackManifest)
	if _, err := c.Capture(context.Background(), "2026.06.01", Options{Base: "2026.01.01"}); err == nil {
		t.Fatal("expected error for an uninstalled base release")
	}
}

func TestCaptureMissingPinnedSystemPackageFails(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)
	c.DpkgVersion = func(_ context.Context, name string) (string, bool, error) {
		if name == "tt-flash" {
			return "", false, nil // not installed
		}
		v, ok := installedDpkgVersions[name]
		return v, ok, nil
	}
	if _, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true}); err == nil {
		t.Fatal("expected error when a pinned system package is not installed")
	}
}

func TestCaptureMissingOptionalSystemPackageOmitted(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)
	c.DpkgVersion = func(_ context.Context, name string) (string, bool, error) {
		if name == "tt-metalium" {
			return "", false, nil // optional, not installed
		}
		v, ok := installedDpkgVersions[name]
		return v, ok, nil
	}
	res, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true})
	if err != nil {
		t.Fatalf("optional missing package must not fail: %v", err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(res.ManifestJSON, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.SystemPackages["metalium"]; ok {
		t.Error("uninstalled optional package metalium must be omitted, not inherited from base")
	}
}

func TestCaptureMissingPipPackageFails(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)
	c.PipShowVersion = func(_ context.Context, _, pkg string) (string, bool, error) {
		if pkg == "tt-umd" {
			return "", false, nil
		}
		v, ok := installedPipVersions[pkg]
		return v, ok, nil
	}
	if _, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true}); err == nil {
		t.Fatal("expected error when a required pip package is not installed")
	}
}

func TestCaptureRejectsNonAptManifest(t *testing.T) {
	root, osRelease := setupRoot(t)
	mustWrite(t, filepath.Join(root, "manifests", "ubuntu-24.04.env"),
		strings.Replace(captureOSManifest, `PKG_MANAGER="apt"`, `PKG_MANAGER="dnf"`, 1))
	c := newCapturer(t, root, osRelease)
	if _, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true}); err == nil {
		t.Fatal("expected error for a non-apt OS manifest")
	}
}

// installBase marks an additional dated release installed with the same
// virtualenv python and git clones as the primary base fixture.
func installBase(t *testing.T, root, release string) {
	t.Helper()
	mustWrite(t, filepath.Join(root, "releases", release+".json"), baseStackManifest)
	versionDir := filepath.Join(root, "versions", release)
	mustWrite(t, filepath.Join(versionDir, ".tt-env-installed"), `{}`)
	mustWrite(t, filepath.Join(versionDir, "venv", "bin", "python"), "#!/bin/sh\n")
	for name := range installedGitHeads {
		mustWrite(t, filepath.Join(versionDir, "src", name, ".git", "HEAD"), "ref: refs/heads/main\n")
	}
}

func TestCaptureSelectsLatestInstalledNonTargetBase(t *testing.T) {
	root, osRelease := setupRoot(t)
	// A newer installed base must win over the older setupRoot base.
	installBase(t, root, "2026.05.20")
	// A still-newer manifest that is NOT installed must be ignored.
	mustWrite(t, filepath.Join(root, "releases", "2026.05.25.json"), baseStackManifest)
	c := newCapturer(t, root, osRelease)

	res, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if res.BaseRelease != "2026.05.20" {
		t.Errorf("base = %q, want 2026.05.20 (latest installed non-target)", res.BaseRelease)
	}
}

func TestCaptureMissingGitCloneFails(t *testing.T) {
	root, osRelease := setupRoot(t)
	if err := os.RemoveAll(filepath.Join(root, "versions", baseRelease, "src", "tt-studio")); err != nil {
		t.Fatalf("remove git clone: %v", err)
	}
	c := newCapturer(t, root, osRelease)
	if _, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true}); err == nil {
		t.Fatal("expected error when a base git component clone is missing")
	}
}

func TestCaptureRendersEmptySectionsAsObjects(t *testing.T) {
	root, osRelease := setupRoot(t)
	// A base manifest with no git or container components.
	noOptional := `{
  "release": "2026.05.16",
  "description": "base stack",
  "components": {"tt-kmd": "ttkmd-2.0.0", "tt-smi": "v1.0.0", "firmware": "v19.6.0", "tt-metal": "v0.70.1"},
  "system_packages": {"kmd": "2.0.0", "smi": "1.0.0", "flash": "1.0.0", "topology": "1.0.0", "metalium": "0.1.0"},
  "python_packages": {"tt-smi": "1.0.0", "tt-umd": "0.1.0", "textual": "0.1.0", "elasticsearch": "8.0.0", "tt-burnin": "0.1.0"},
  "git_components": {},
  "container_components": {}
}`
	mustWrite(t, filepath.Join(root, "releases", baseRelease+".json"), noOptional)
	c := newCapturer(t, root, osRelease)

	res, err := c.Capture(context.Background(), "2026.06.01", Options{DryRun: true})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if strings.Contains(string(res.ManifestJSON), "null") {
		t.Errorf("manifest must not contain JSON null sections:\n%s", res.ManifestJSON)
	}
	if !strings.Contains(string(res.ManifestJSON), `"git_components": {}`) {
		t.Errorf("empty git_components must render as an object:\n%s", res.ManifestJSON)
	}
	if !strings.Contains(string(res.ManifestJSON), `"container_components": {}`) {
		t.Errorf("empty container_components must render as an object:\n%s", res.ManifestJSON)
	}
}

// TestCaptureFromDecouplesProbeTree verifies that --from probes a different
// installed release tree than the --base manifest, and that the base manifest
// need not be installed when an explicit probe release is given.
func TestCaptureFromDecouplesProbeTree(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)

	const probeRelease = "2026.06.02"
	// Install only the probe tree (venv + git clones); it has no manifest.
	probeDir := filepath.Join(root, "versions", probeRelease)
	mustWrite(t, filepath.Join(probeDir, ".tt-env-installed"), `{}`)
	mustWrite(t, filepath.Join(probeDir, "venv", "bin", "python"), "#!/bin/sh\n")
	for name := range installedGitHeads {
		mustWrite(t, filepath.Join(probeDir, "src", name, ".git", "HEAD"), "ref: refs/heads/main\n")
	}

	// Remove the base installed tree to prove the base need not be installed
	// when --from supplies the probe tree.
	if err := os.RemoveAll(filepath.Join(root, "versions", baseRelease)); err != nil {
		t.Fatalf("remove base tree: %v", err)
	}

	res, err := c.Capture(context.Background(), "2026.06.05", Options{
		Base:         baseRelease,
		ProbeRelease: probeRelease,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("Capture --from: %v", err)
	}
	if res.BaseRelease != baseRelease {
		t.Errorf("base = %q, want %q", res.BaseRelease, baseRelease)
	}
	if res.ProbeRelease != probeRelease {
		t.Errorf("probe = %q, want %q", res.ProbeRelease, probeRelease)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(res.ManifestJSON, &m); err != nil {
		t.Fatalf("rendered manifest invalid: %v", err)
	}
	if m.PythonPackages["tt-smi"] != "5.2.0" {
		t.Errorf("python_packages.tt-smi = %q, want 5.2.0", m.PythonPackages["tt-smi"])
	}
	if m.GitComponents["tt-studio"].Version != installedGitHeads["tt-studio"] {
		t.Errorf("git tt-studio version = %q", m.GitComponents["tt-studio"].Version)
	}
}

// TestCaptureFromRequiresInstalledProbe verifies that an explicit --from release
// must itself be installed, even when the base manifest exists.
func TestCaptureFromRequiresInstalledProbe(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)

	if _, err := c.Capture(context.Background(), "2026.06.05", Options{
		Base:         baseRelease,
		ProbeRelease: "2099.01.01",
		DryRun:       true,
	}); err == nil {
		t.Fatal("expected error for an uninstalled probe release")
	}
}

func TestCaptureFromDescriptionNotesProbeProvenance(t *testing.T) {
	root, osRelease := setupRoot(t)
	c := newCapturer(t, root, osRelease)

	const probeRelease = "2026.06.02"
	probeDir := filepath.Join(root, "versions", probeRelease)
	mustWrite(t, filepath.Join(probeDir, ".tt-env-installed"), `{}`)
	mustWrite(t, filepath.Join(probeDir, "venv", "bin", "python"), "#!/bin/sh\n")
	for name := range installedGitHeads {
		mustWrite(t, filepath.Join(probeDir, "src", name, ".git", "HEAD"), "ref: refs/heads/main\n")
	}

	res, err := c.Capture(context.Background(), "2026.06.05", Options{
		Base:         baseRelease,
		ProbeRelease: probeRelease,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("Capture --from: %v", err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(res.ManifestJSON, &m); err != nil {
		t.Fatalf("rendered manifest invalid: %v", err)
	}
	if !strings.Contains(m.Description, probeRelease) || !strings.Contains(m.Description, baseRelease) {
		t.Errorf("description should mention base %q and probe %q, got %q", baseRelease, probeRelease, m.Description)
	}
}
