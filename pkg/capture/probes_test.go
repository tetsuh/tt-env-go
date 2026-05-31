package capture

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// runExit returns a genuine *exec.ExitError with the given exit code, so probe
// tests can exercise the exit-code classification path.
func runExit(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	if err == nil {
		t.Fatalf("expected a non-nil exit error for code %d", code)
	}
	return err
}

func TestDefaultDpkgVersion(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("ii  1.2.3-1\n"), nil
	}
	c := &Capturer{Runner: runner}
	v, ok, err := c.defaultDpkgVersion(context.Background(), "pkg")
	if err != nil || !ok || v != "1.2.3-1" {
		t.Fatalf("got %q, %v, %v", v, ok, err)
	}
}

func TestDefaultDpkgVersionResidualConfig(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		// "rc" = removed, residual config files remain: not installed.
		return []byte("rc  1.2.3-1\n"), nil
	}
	c := &Capturer{Runner: runner}
	if _, ok, err := c.defaultDpkgVersion(context.Background(), "pkg"); err != nil || ok {
		t.Fatalf("residual-config package must be not-installed, got ok=%v err=%v", ok, err)
	}
}

func TestDefaultDpkgVersionNotInstalled(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, runExit(t, 1) // dpkg-query exits 1 for unknown packages
	}
	c := &Capturer{Runner: runner}
	_, ok, err := c.defaultDpkgVersion(context.Background(), "pkg")
	if err != nil || ok {
		t.Fatalf("expected not-installed, got ok=%v err=%v", ok, err)
	}
}

func TestDefaultDpkgVersionPropagatesTransientError(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, context.Canceled // could not run the probe at all
	}
	c := &Capturer{Runner: runner}
	if _, _, err := c.defaultDpkgVersion(context.Background(), "pkg"); err == nil {
		t.Fatal("expected transient runner error to propagate, got nil")
	}
}

func TestDefaultPipShowVersion(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Name: tt-smi\nVersion: 5.2.0\nSummary: x\n"), nil
	}
	c := &Capturer{Runner: runner}
	v, ok, err := c.defaultPipShowVersion(context.Background(), "/venv/python", "tt-smi")
	if err != nil || !ok || v != "5.2.0" {
		t.Fatalf("got %q, %v, %v", v, ok, err)
	}
}

func TestDefaultPipShowVersionRejectsMalformed(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Version: bad version!!\n"), nil
	}
	c := &Capturer{Runner: runner}
	if _, _, err := c.defaultPipShowVersion(context.Background(), "/venv/python", "tt-smi"); err == nil {
		t.Fatal("expected error for malformed pip version")
	}
}

func TestDefaultPipShowVersionNotInstalled(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, runExit(t, 1) // pip show exits 1 when the package is absent
	}
	c := &Capturer{Runner: runner}
	_, ok, err := c.defaultPipShowVersion(context.Background(), "/venv/python", "tt-smi")
	if err != nil || ok {
		t.Fatalf("expected not-installed, got ok=%v err=%v", ok, err)
	}
}

func TestDefaultPipShowVersionPropagatesTransientError(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, context.Canceled
	}
	c := &Capturer{Runner: runner}
	if _, _, err := c.defaultPipShowVersion(context.Background(), "/venv/python", "tt-smi"); err == nil {
		t.Fatal("expected transient runner error to propagate, got nil")
	}
}

func TestDefaultGitHead(t *testing.T) {
	sha := "a6d347af3980540bb16d10ec473a6b09ce6f2138"
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte(sha + "\n"), nil
	}
	c := &Capturer{Runner: runner}
	head, err := c.defaultGitHead(context.Background(), "/repo")
	if err != nil || head != sha {
		t.Fatalf("got %q, %v", head, err)
	}
}

func TestDefaultGitHeadRejectsNonSHA(t *testing.T) {
	runner := &packagemanager.MockRunner{}
	runner.RunFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("not-a-sha\n"), nil
	}
	c := &Capturer{Runner: runner}
	if _, err := c.defaultGitHead(context.Background(), "/repo"); err == nil {
		t.Fatal("expected error for non-SHA git HEAD")
	}
}
