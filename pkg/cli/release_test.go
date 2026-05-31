package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/version"
)

func installRelease(t *testing.T, root, release string) {
	t.Helper()
	inst := &version.Installer{Root: root}
	if _, err := inst.Install(release, func(string) error { return nil }); err != nil {
		t.Fatalf("Install(%s) error = %v", release, err)
	}
}

func writeManifest(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUseCommandSwitchesActiveRelease(t *testing.T) {
	root := t.TempDir()
	installRelease(t, root, "2026.05.16")
	t.Setenv("TT_HOME", root)

	buf := new(bytes.Buffer)
	useCmd.SetOut(buf)
	t.Cleanup(func() { useCmd.SetOut(nil) })

	if err := runUse(useCmd, "2026.05.16"); err != nil {
		t.Fatalf("runUse() error = %v", err)
	}
	if !strings.Contains(buf.String(), "Now using release 2026.05.16.") {
		t.Errorf("unexpected output: %q", buf.String())
	}

	inst := &version.Installer{Root: root}
	active, err := inst.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if active != "2026.05.16" {
		t.Errorf("active release = %q, want %q", active, "2026.05.16")
	}
}

func TestUseCommandRejectsUninstalledRelease(t *testing.T) {
	t.Setenv("TT_HOME", t.TempDir())
	if err := runUse(useCmd, "2026.05.16"); err == nil {
		t.Error("expected error for uninstalled release")
	}
}

func TestRemoveCommandUninstallsRelease(t *testing.T) {
	root := t.TempDir()
	installRelease(t, root, "2026.05.16")
	t.Setenv("TT_HOME", root)

	buf := new(bytes.Buffer)
	removeCmd.SetOut(buf)
	t.Cleanup(func() { removeCmd.SetOut(nil) })

	if err := runRemove(removeCmd, "2026.05.16"); err != nil {
		t.Fatalf("runRemove() error = %v", err)
	}
	if !strings.Contains(buf.String(), "Removed release 2026.05.16.") {
		t.Errorf("unexpected output: %q", buf.String())
	}
	if (&version.Installer{Root: root}).IsInstalled("2026.05.16") {
		t.Error("release should be uninstalled after remove command")
	}
}

func TestRemoveCommandRejectsUninstalledRelease(t *testing.T) {
	t.Setenv("TT_HOME", t.TempDir())
	if err := runRemove(removeCmd, "2026.05.16"); err == nil {
		t.Error("expected error removing an uninstalled release")
	}
}

func TestListCommandMarksInstalledAndAvailable(t *testing.T) {
	root := t.TempDir()
	installRelease(t, root, "2026.04.01") // installed
	releasesDir := filepath.Join(root, "releases")
	writeManifest(t, releasesDir, "a.json", `{"release":"2026.04.01"}`)
	writeManifest(t, releasesDir, "b.json", `{"release":"2026.05.16"}`)
	writeManifest(t, releasesDir, "bad.json", `{invalid`)
	t.Setenv("TT_HOME", root)

	out := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	listCmd.SetOut(out)
	listCmd.SetErr(errBuf)
	t.Cleanup(func() {
		listCmd.SetOut(nil)
		listCmd.SetErr(nil)
	})

	if err := runList(listCmd); err != nil {
		t.Fatalf("runList() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"Releases", "2026.04.01 [installed]", "2026.05.16 [available]"} {
		if !strings.Contains(got, want) {
			t.Errorf("list output missing %q\n%s", want, got)
		}
	}
	// Sorted ascending: installed release listed before the available one.
	if strings.Index(got, "2026.04.01") > strings.Index(got, "2026.05.16") {
		t.Errorf("releases not sorted ascending\n%s", got)
	}
	if !strings.Contains(errBuf.String(), "bad.json") {
		t.Errorf("expected warning for invalid manifest, got %q", errBuf.String())
	}
}

func TestListCommandEmptyCatalog(t *testing.T) {
	t.Setenv("TT_HOME", t.TempDir())

	out := new(bytes.Buffer)
	listCmd.SetOut(out)
	t.Cleanup(func() { listCmd.SetOut(nil) })

	if err := runList(listCmd); err != nil {
		t.Fatalf("runList() error = %v", err)
	}
	if !strings.Contains(out.String(), "(none)") {
		t.Errorf("expected (none) for empty catalog, got %q", out.String())
	}
}

func TestListCommandIgnoresNonJSONFiles(t *testing.T) {
	root := t.TempDir()
	releasesDir := filepath.Join(root, "releases")
	writeManifest(t, releasesDir, "a.json", `{"release":"2026.04.01"}`)
	writeManifest(t, releasesDir, "notes.txt", `not a manifest`)
	t.Setenv("TT_HOME", root)

	out := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	listCmd.SetOut(out)
	listCmd.SetErr(errBuf)
	t.Cleanup(func() {
		listCmd.SetOut(nil)
		listCmd.SetErr(nil)
	})

	if err := runList(listCmd); err != nil {
		t.Fatalf("runList() error = %v", err)
	}
	if strings.Contains(errBuf.String(), "notes.txt") {
		t.Errorf("non-json file should be ignored, not warned: %q", errBuf.String())
	}
	if !strings.Contains(out.String(), "2026.04.01 [available]") {
		t.Errorf("expected the json manifest listed, got %q", out.String())
	}
}

func TestUseAndListCommandArgs(t *testing.T) {
	if useCmd.Args == nil || listCmd.Args == nil {
		t.Fatal("use/list commands must declare Args validators")
	}
	if err := useCmd.Args(useCmd, []string{}); err == nil {
		t.Error("expected use to reject zero arguments")
	}
	if err := useCmd.Args(useCmd, []string{"a", "b"}); err == nil {
		t.Error("expected use to reject two arguments")
	}
	if err := listCmd.Args(listCmd, []string{"extra"}); err == nil {
		t.Error("expected list to reject positional arguments")
	}
	if removeCmd.Args == nil {
		t.Fatal("remove command must declare an Args validator")
	}
	if err := removeCmd.Args(removeCmd, []string{}); err == nil {
		t.Error("expected remove to reject zero arguments")
	}
	if err := removeCmd.Args(removeCmd, []string{"a", "b"}); err == nil {
		t.Error("expected remove to reject two arguments")
	}
}

func TestUseCommandExecutesViaCobra(t *testing.T) {
	root := t.TempDir()
	installRelease(t, root, "2026.05.16")
	t.Setenv("TT_HOME", root)

	buf := new(bytes.Buffer)
	useCmd.SetOut(buf)
	t.Cleanup(func() { useCmd.SetOut(nil) })

	if err := useCmd.RunE(useCmd, []string{"2026.05.16"}); err != nil {
		t.Fatalf("useCmd.RunE() error = %v", err)
	}
	inst := &version.Installer{Root: root}
	if active, err := inst.Current(); err != nil || active != "2026.05.16" {
		t.Errorf("Current() = %q, %v; want 2026.05.16, nil", active, err)
	}
}
