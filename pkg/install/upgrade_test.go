package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingManifestSuggestsLikeAndListsCandidates(t *testing.T) {
	root, osRelease := setupRoot(t)
	mustWrite(t, filepath.Join(root, "releases.local", "2026.04.01.json"),
		strings.Replace(testStackManifest, `"release": "2026.05.16"`, `"release": "2026.04.01"`, 1))
	runner := latestAwareRunner()
	orch := withProbes(&Orchestrator{Root: root, OSReleasePath: osRelease, Runner: runner})
	for _, opts := range []Options{{Upgrade: true}, {Upgrade: true, Like: "0.76.0"}} {
		_, err := orch.Install(context.Background(), "0.77.0", opts)
		if err == nil {
			t.Fatal("expected missing manifest error")
		}
		msg := err.Error()
		for _, want := range []string{"--upgrade --like <release>", "available releases: 2026.04.01, 2026.05.16"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error missing %q: %v", want, err)
			}
		}
	}
	if len(runner.Commands) != 0 {
		t.Errorf("missing manifest ran commands: %v", runner.Commands)
	}
}

func TestUpgradeSeededReleaseRequiresExplicitLikeOnRefresh(t *testing.T) {
	root, osRelease := setupRoot(t)
	runner := latestAwareRunner()
	orch := withProbes(&Orchestrator{Root: root, OSReleasePath: osRelease, Runner: runner})
	const target = "0.77.0"
	if _, err := orch.Install(context.Background(), target, Options{Upgrade: true, Like: testRelease}); err != nil {
		t.Fatalf("initial seeded upgrade: %v", err)
	}
	before := len(runner.Commands)
	_, err := orch.Install(context.Background(), target, Options{Upgrade: true, Force: true})
	if err == nil || !strings.Contains(err.Error(), "--upgrade --like <release>") {
		t.Fatalf("refresh without a target manifest should require --like, got: %v", err)
	}
	if len(runner.Commands) != before {
		t.Errorf("refresh without --like ran commands: %v", runner.Commands[before:])
	}
	if _, err := orch.Install(context.Background(), target, Options{Upgrade: true, Force: true, Like: testRelease}); err != nil {
		t.Fatalf("explicit template refresh: %v", err)
	}
}

func TestUpgradeDryRunAndInstallShareScopeSummary(t *testing.T) {
	root, osRelease := setupRoot(t)
	// The template does not declare optional metalium; upgrade must explain
	// that it is skipped instead of silently installing it.
	noMetalium := strings.Replace(testStackManifest, `,
    "metalium": "5.0.0"`, ``, 1)
	if noMetalium == testStackManifest {
		t.Fatal("fixture did not remove metalium")
	}
	mustWrite(t, filepath.Join(root, "releases", testRelease+".json"), noMetalium)
	var logs []string
	orch := withProbes(&Orchestrator{
		Root: root, OSReleasePath: osRelease, Runner: latestAwareRunner(),
		Logf: func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) },
	})
	const target = "0.77.0"
	opts := Options{Upgrade: true, Like: testRelease, DryRun: true}
	if _, err := orch.Install(context.Background(), target, opts); err != nil {
		t.Fatalf("upgrade dry-run: %v", err)
	}
	dry := resolutionSummary(logs)
	if _, err := os.Stat(filepath.Join(root, "versions", target)); !os.IsNotExist(err) {
		t.Errorf("dry-run staged the target: %v", err)
	}
	logs = nil
	opts.DryRun = false
	if _, err := orch.Install(context.Background(), target, opts); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	installedSummary := resolutionSummary(logs)
	if dry != installedSummary {
		t.Errorf("scope summary differs between dry-run and install:\ndry: %s\ninstalled: %s", dry, installedSummary)
	}
	for _, want := range []string{
		"re-resolve: system and Python packages", "git components at remote HEAD",
		"keep: container references from the template manifest (no container digest refresh)",
		"optional metalium: skipped (not declared)", "ghcr.io/tenstorrent/tt-metalium@sha256:abc123",
	} {
		if !strings.Contains(installedSummary, want) {
			t.Errorf("summary missing %q: %s", want, installedSummary)
		}
	}
}

func resolutionSummary(logs []string) string {
	var lines []string
	for _, line := range logs {
		if strings.HasPrefix(line, "  ") || strings.Contains(line, ": package resolution:") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
