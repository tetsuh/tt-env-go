package capture

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// pipVersionRe constrains a captured PyPI/pip version to the characters proto1
// accepts, guarding against malformed `pip show` output.
var pipVersionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.!+-]*$`)

// gitHeadRe matches a full 40-character git object name.
var gitHeadRe = regexp.MustCompile(`^[A-Fa-f0-9]{40}$`)

// exitCode reports the process exit code when err is an *exec.ExitError (the
// command ran and exited non-zero). The second return is false when the command
// could not be run at all (binary missing, context cancellation, etc.), which
// must be surfaced rather than interpreted as "not installed".
func exitCode(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// defaultDpkgVersion returns the installed version of a dpkg package via
// `dpkg-query`. The package status abbreviation is checked so that a package
// left in a residual-config ("rc") or otherwise not-installed state yields
// ("", false, nil). dpkg-query exits with code 1 when the package is unknown,
// which is reported as not installed; any other failure (other exit codes,
// missing binary, context cancellation) is returned as an error.
func (c *Capturer) defaultDpkgVersion(ctx context.Context, name string) (string, bool, error) {
	out, err := c.runner().Run(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", "--", name)
	if err != nil {
		if code, ok := exitCode(err); ok && code == 1 {
			return "", false, nil // package not found
		}
		return "", false, fmt.Errorf("capture: dpkg-query %q: %w", name, err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return "", false, nil
	}
	// The status abbreviation's second character is the current state; 'i'
	// means installed (e.g. "ii"). Anything else ("rc", "un", "pn") is not.
	status, version := fields[0], fields[1]
	if len(status) < 2 || status[1] != 'i' {
		return "", false, nil
	}
	return version, true, nil
}

// defaultPipShowVersion returns the installed version of a pip package within
// the given virtualenv python, parsing `pip show` output. `pip show` exits with
// code 1 when the package is absent, which yields ("", false, nil); any other
// failure to run pip is returned as an error.
func (c *Capturer) defaultPipShowVersion(ctx context.Context, venvPython, pkg string) (string, bool, error) {
	out, err := c.runner().Run(ctx, venvPython, "-m", "pip", "show", "--", pkg)
	if err != nil {
		if code, ok := exitCode(err); ok && code == 1 {
			return "", false, nil // package not found
		}
		return "", false, fmt.Errorf("capture: pip show %q: %w", pkg, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "Version:")
		if !ok {
			continue
		}
		version := strings.TrimSpace(rest)
		if version == "" {
			return "", false, nil
		}
		if !pipVersionRe.MatchString(version) {
			return "", false, fmt.Errorf("capture: invalid pip version for %q: %q", pkg, version)
		}
		return version, true, nil
	}
	return "", false, nil
}

// defaultGitHead returns the checked-out HEAD commit of a local git clone.
func (c *Capturer) defaultGitHead(ctx context.Context, repoDir string) (string, error) {
	out, err := c.runner().Run(ctx, "git", "-C", repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("capture: read git HEAD in %s: %w", repoDir, err)
	}
	head := strings.TrimSpace(string(out))
	if !gitHeadRe.MatchString(head) {
		return "", fmt.Errorf("capture: unexpected git HEAD in %s: %q", repoDir, head)
	}
	return head, nil
}
