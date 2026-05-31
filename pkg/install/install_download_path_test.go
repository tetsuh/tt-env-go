package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

const downloadOSManifest = `PKG_MANAGER="apt"
USE_SYSTEM_PACKAGES="false"
`

// downloadStackManifest builds a stack manifest whose single component is
// downloaded and checksum-verified against the given payload.
func downloadStackManifest(downloadURL, sha string) string {
	return fmt.Sprintf(`{
  "release": "2026.05.16",
  "description": "download path test",
  "components": {
    "tt-kmd": {"version": "1.0.0", "download_url": %q, "sha256": %q}
  },
  "git_components": {
    "tt-foo": {"url": "https://github.com/tenstorrent/tt-foo.git", "version": "v1.0.0"}
  }
}`, downloadURL, sha)
}

func TestInstallDownloadPathDryRunValidates(t *testing.T) {
	// A component missing its sha256 must fail even in dry-run, matching proto1
	// which validates download metadata before performing any work.
	root := t.TempDir()
	manifestJSON := `{
  "release": "2026.05.16",
  "description": "invalid download manifest",
  "components": {
    "tt-kmd": {"version": "1.0.0", "download_url": "https://example.test/a"}
  }
}`
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"), manifestJSON)
	mustWrite(t, filepath.Join(root, "manifests", "ubuntu-24.04.env"), downloadOSManifest)
	osRelease := filepath.Join(root, "os-release")
	mustWrite(t, osRelease, testOSRelease)

	orch := &Orchestrator{Root: root, OSReleasePath: osRelease, Logf: func(string, ...any) {}}
	if _, err := orch.Install(context.Background(), testRelease, Options{DryRun: true}); err == nil {
		t.Fatal("expected dry-run to reject a component missing sha256")
	}
}

func TestInstallDownloadPath(t *testing.T) {
	body := []byte("kmd-artifact")
	url := "https://example.test/kmd.tar.gz"
	sha := sha256Hex(body)

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"), downloadStackManifest(url, sha))
	mustWrite(t, filepath.Join(root, "manifests", "ubuntu-24.04.env"), downloadOSManifest)
	osRelease := filepath.Join(root, "os-release")
	mustWrite(t, osRelease, testOSRelease)

	// Runner handles both curl (download path) and git clone (git components).
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "curl":
			var out string
			for i, a := range args {
				if a == "--output" && i+1 < len(args) {
					out = args[i+1]
				}
			}
			if out != "" {
				_ = os.WriteFile(out, body, 0o644)
			}
			return nil, nil
		case name == "git" && len(args) > 0 && args[0] == "clone":
			dest := args[len(args)-1]
			_ = os.MkdirAll(dest, 0o755)
			_ = os.WriteFile(filepath.Join(dest, "run.py"), []byte("#!/usr/bin/env python\n"), 0o755)
			return nil, nil
		case name == "git" && containsArg(args, "rev-parse"):
			return []byte("0123456789abcdef0123456789abcdef01234567\n"), nil
		}
		return nil, nil
	}

	orch := &Orchestrator{Root: root, Runner: runner, OSReleasePath: osRelease, Logf: func(string, ...any) {}}
	res, err := orch.Install(context.Background(), testRelease, Options{})
	if err != nil {
		t.Fatalf("Install download path: %v", err)
	}
	if !res.Installed {
		t.Fatalf("expected Installed=true, got %+v", res)
	}

	versionDir := filepath.Join(root, "versions", testRelease)
	// Downloaded artifact and git wrapper present.
	if _, err := os.Stat(filepath.Join(versionDir, "artifacts", "tt-kmd")); err != nil {
		t.Errorf("expected downloaded artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(versionDir, "bin", "tt-foo")); err != nil {
		t.Errorf("expected git component wrapper: %v", err)
	}

	// The download path must not install system packages or provision a venv.
	assertCommandSeen(t, runner, "curl", "--fail")
	for _, c := range runner.Commands {
		if c.Name == "sudo" {
			t.Errorf("download path must not run system package commands, got %v", c)
		}
	}
	if _, err := os.Stat(filepath.Join(versionDir, "venv")); !os.IsNotExist(err) {
		t.Errorf("download path must not provision a venv")
	}
}
