// Package gitclone clones the git_components declared in a release manifest and
// checks each one out at its pinned commit.
//
// Cloning runs through the package-manager CommandRunner abstraction so it can
// be unit tested without a real network or git binary. Each component is cloned
// with "git clone --filter=blob:none", fetched, checked out in detached HEAD at
// the pinned version, and then verified so a checkout that does not land on the
// pinned commit fails clearly.
package gitclone

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// DefaultGit is the git executable used when Cloner does not override it.
const DefaultGit = "git"

// Component describes a git-sourced component to clone. Version may hold either
// a tag or a commit SHA; it is resolved to a commit during verification.
type Component struct {
	URL     string
	Version string
}

// Cloner clones git components and checks them out at their pinned commit.
type Cloner struct {
	// Runner executes the underlying git commands. If nil, ExecRunner is used.
	Runner packagemanager.CommandRunner
	// Git is the git executable to invoke. If empty, DefaultGit is used.
	Git string
}

func (c *Cloner) runner() packagemanager.CommandRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return packagemanager.ExecRunner{}
}

func (c *Cloner) git() string {
	if c.Git != "" {
		return c.Git
	}
	return DefaultGit
}

// Provision clones every component under srcDir and checks each out at its
// pinned commit. It is idempotent: an already-cloned component whose origin
// matches is fetched and re-checked-out rather than re-cloned. It returns nil
// without running any command when components is empty.
func (c *Cloner) Provision(ctx context.Context, srcDir string, components map[string]Component) error {
	if srcDir == "" {
		return errors.New("gitclone: source directory must not be empty")
	}
	if len(components) == 0 {
		return nil
	}

	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := validateComponent(name, components[name]); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return fmt.Errorf("gitclone: create source directory: %w", err)
	}

	for _, name := range names {
		if err := c.clone(ctx, srcDir, name, components[name]); err != nil {
			return err
		}
	}
	return nil
}

// clone clones (or reuses) a single component and checks it out at its pin.
func (c *Cloner) clone(ctx context.Context, srcDir, name string, comp Component) error {
	componentDir := filepath.Join(srcDir, name)

	needClone, err := c.needsClone(ctx, componentDir, comp.URL)
	if err != nil {
		return err
	}
	if needClone {
		args := []string{"clone", "--filter=blob:none", "--", comp.URL, componentDir}
		if out, rErr := c.runner().Run(ctx, c.git(), args...); rErr != nil {
			return commandError(c.git(), args, out, rErr)
		}
	}

	if err := c.runGit(ctx, componentDir, "fetch", "origin"); err != nil {
		return err
	}
	if err := c.runGit(ctx, componentDir, "checkout", "--detach", comp.Version); err != nil {
		return err
	}
	return c.verifyHead(ctx, componentDir, name, comp.Version)
}

// needsClone reports whether componentDir must be cloned. An existing directory
// is reused only when its origin remote matches url; a directory that is not a
// readable git repository or points at a different remote is rejected rather
// than silently overwritten.
func (c *Cloner) needsClone(ctx context.Context, componentDir, url string) (bool, error) {
	info, err := os.Lstat(componentDir)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("gitclone: stat %s: %w", componentDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("gitclone: %s exists and is a symlink", componentDir)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("gitclone: %s exists and is not a directory", componentDir)
	}

	args := []string{"-C", componentDir, "remote", "get-url", "origin"}
	out, rErr := c.runner().Run(ctx, c.git(), args...)
	if rErr != nil {
		return false, fmt.Errorf("gitclone: %s exists but is not a usable git repository: %w", componentDir, rErr)
	}
	current := strings.TrimSpace(string(out))
	if normalizeURL(current) != normalizeURL(url) {
		return false, fmt.Errorf("gitclone: %s origin %q does not match manifest url %q", componentDir, current, url)
	}
	return false, nil
}

// verifyHead confirms the detached HEAD resolves to the same commit as the
// pinned version, resolving the pin through git so a tag or SHA both work.
func (c *Cloner) verifyHead(ctx context.Context, componentDir, name, version string) error {
	headArgs := []string{"-C", componentDir, "rev-parse", "HEAD"}
	headOut, err := c.runner().Run(ctx, c.git(), headArgs...)
	if err != nil {
		return commandError(c.git(), headArgs, headOut, err)
	}
	pinArgs := []string{"-C", componentDir, "rev-parse", "--verify", version + "^{commit}"}
	pinOut, err := c.runner().Run(ctx, c.git(), pinArgs...)
	if err != nil {
		return commandError(c.git(), pinArgs, pinOut, err)
	}

	head := strings.ToLower(strings.TrimSpace(string(headOut)))
	pinned := strings.ToLower(strings.TrimSpace(string(pinOut)))
	if head == "" || pinned == "" {
		return fmt.Errorf("gitclone: %s: could not resolve checked-out commit for version %q", name, version)
	}
	if head != pinned {
		return fmt.Errorf("gitclone: %s: checked-out commit %s does not match pinned %s (%s)", name, head, pinned, version)
	}
	return nil
}

// runGit runs a git subcommand inside componentDir, wrapping any failure.
func (c *Cloner) runGit(ctx context.Context, componentDir string, sub ...string) error {
	args := append([]string{"-C", componentDir}, sub...)
	if out, err := c.runner().Run(ctx, c.git(), args...); err != nil {
		return commandError(c.git(), args, out, err)
	}
	return nil
}

// validateComponent checks a component's name, url, and version.
func validateComponent(name string, comp Component) error {
	if err := validateComponentName(name); err != nil {
		return err
	}
	if err := validateToken("component url", comp.URL); err != nil {
		return err
	}
	return validateToken("component version", comp.Version)
}

// validateComponentName ensures name is a single, safe path element.
func validateComponentName(name string) error {
	if name == "" {
		return errors.New("gitclone: component name must not be empty")
	}
	if filepath.IsAbs(name) || name == "." || name == ".." ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("gitclone: invalid component name %q: must be a single directory name", name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("gitclone: component name must not start with '-': %q", name)
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("gitclone: component name must not contain whitespace or control characters: %q", name)
		}
	}
	return nil
}

// validateToken rejects empty values, leading dashes (which could be parsed as
// command options), and whitespace or control characters.
func validateToken(label, value string) error {
	if value == "" {
		return fmt.Errorf("gitclone: %s must not be empty", label)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("gitclone: %s must not start with '-': %q", label, value)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("gitclone: %s must not contain whitespace or control characters: %q", label, value)
		}
	}
	return nil
}

// normalizeURL strips a trailing slash and ".git" suffix so equivalent remote
// URLs compare equal.
func normalizeURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	return strings.TrimSuffix(url, "/")
}

// commandError wraps an execution failure with the command line and any output.
func commandError(name string, args []string, out []byte, err error) error {
	cmd := name
	if len(args) > 0 {
		cmd += " " + strings.Join(args, " ")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return fmt.Errorf("%s: %w: %s", cmd, err, trimmed)
}
