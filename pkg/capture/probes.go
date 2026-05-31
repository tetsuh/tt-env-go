package capture

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// pipVersionRe constrains a captured PyPI/pip version to the characters proto1
// accepts, guarding against malformed `pip show` output.
var pipVersionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.!+-]*$`)

// gitHeadRe matches a full 40-character git object name.
var gitHeadRe = regexp.MustCompile(`^[A-Fa-f0-9]{40}$`)

// defaultDpkgVersion returns the installed version of a dpkg package via
// `dpkg-query`. The package status abbreviation is checked so that a package
// left in a residual-config ("rc") or otherwise not-installed state yields
// ("", false, nil), letting the caller apply its pinned/optional policy. An
// error running dpkg-query (including the non-zero exit dpkg-query uses for
// unknown packages) is likewise treated as not installed.
func (c *Capturer) defaultDpkgVersion(ctx context.Context, name string) (string, bool, error) {
	out, err := c.runner().Run(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev} ${Version}", "--", name)
	if err != nil {
		// dpkg-query exits non-zero when the package is not installed.
		return "", false, nil
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
// the given virtualenv python, parsing `pip show` output. A package that is not
// installed yields ("", false, nil).
func (c *Capturer) defaultPipShowVersion(ctx context.Context, venvPython, pkg string) (string, bool, error) {
	out, err := c.runner().Run(ctx, venvPython, "-m", "pip", "show", "--", pkg)
	if err != nil {
		return "", false, nil
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
