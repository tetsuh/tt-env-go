// Package capture implements `tt-env capture`: it probes an installed stack
// release and writes a local-only release manifest snapshotting the versions
// actually present on the machine.
//
// Unlike proto1's capture_release, which records the latest *available* versions
// (apt-cache madison, PyPI latest, git remote HEAD), this engine records the
// *installed* versions: dpkg-query for system packages, `pip show` within the
// base release virtualenv for python packages, and the local git HEAD of each
// cloned git component. The base release therefore must already be installed
// under Root/versions/<base>; its manifest supplies the component structure
// (which packages, repos, and containers exist) that the probed versions fill
// in. Container component digests are copied from the base release unchanged
// (ghcr probing is handled separately).
package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
	"github.com/tetsuh/tt-env-go/pkg/stackpolicy"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// osManifestSuffix is the file extension of OS manifests under Root/manifests.
const osManifestSuffix = ".env"

// datedReleaseRe matches the YYYY.MM.DD release names that are eligible to be a
// default capture base.
var datedReleaseRe = regexp.MustCompile(`^[0-9]{4}\.[0-9]{2}\.[0-9]{2}$`)

// Capturer captures local-only release manifests under a TT_HOME root.
type Capturer struct {
	// Root is the TT_HOME directory under which releases/, manifests/, and
	// versions/ live.
	Root string
	// Runner executes the dpkg/pip/git probe commands. When nil, an exec runner
	// is used.
	Runner packagemanager.CommandRunner
	// OSReleasePath overrides the os-release path used for OS detection. When
	// empty, manifest.DetectOS uses its default.
	OSReleasePath string
	// Logf logs informational progress. When nil, slog.Info is used.
	Logf func(format string, args ...any)

	// The probe functions below are injectable for testing; when nil the
	// runner-backed defaults are used.
	DpkgVersion    func(ctx context.Context, name string) (string, bool, error)
	PipShowVersion func(ctx context.Context, venvPython, pkg string) (string, bool, error)
	GitHead        func(ctx context.Context, repoDir string) (string, error)
}

// Options modifies a capture.
type Options struct {
	// Base is the release whose installed tree and manifest seed the capture.
	// When empty, the latest installed dated release is used.
	Base string
	// DryRun renders the manifest without writing it.
	DryRun bool
	// Force overwrites an existing target manifest.
	Force bool
}

// Result reports the outcome of a capture.
type Result struct {
	Release     string
	BaseRelease string
	Path        string
	Written     bool
	// ManifestJSON is the rendered manifest, always populated.
	ManifestJSON []byte
}

func (c *Capturer) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	slog.Info(fmt.Sprintf(format, args...))
}

func (c *Capturer) runner() packagemanager.CommandRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return packagemanager.ExecRunner{}
}

func (c *Capturer) dpkgVersion(ctx context.Context, name string) (string, bool, error) {
	if c.DpkgVersion != nil {
		return c.DpkgVersion(ctx, name)
	}
	return c.defaultDpkgVersion(ctx, name)
}

func (c *Capturer) pipShowVersion(ctx context.Context, venvPython, pkg string) (string, bool, error) {
	if c.PipShowVersion != nil {
		return c.PipShowVersion(ctx, venvPython, pkg)
	}
	return c.defaultPipShowVersion(ctx, venvPython, pkg)
}

func (c *Capturer) gitHead(ctx context.Context, repoDir string) (string, error) {
	if c.GitHead != nil {
		return c.GitHead(ctx, repoDir)
	}
	return c.defaultGitHead(ctx, repoDir)
}

// Capture probes the installed base release and renders (and, unless DryRun,
// writes) a local-only manifest for release.
func (c *Capturer) Capture(ctx context.Context, release string, opts Options) (Result, error) {
	if err := version.ValidateRelease(release); err != nil {
		return Result{}, err
	}

	inst := &version.Installer{Root: c.Root}
	target := filepath.Join(c.Root, "releases", release+".json")
	if !opts.DryRun && !opts.Force {
		if _, err := os.Stat(target); err == nil {
			return Result{}, fmt.Errorf("capture: release manifest already exists: %s (use --force to overwrite)", target)
		}
	}

	baseRelease, baseManifest, err := c.resolveBase(release, opts.Base, inst)
	if err != nil {
		return Result{}, err
	}

	osm, err := c.resolveOSManifest()
	if err != nil {
		return Result{}, err
	}

	systemPackages, err := c.captureSystemPackages(ctx, osm, baseManifest)
	if err != nil {
		return Result{}, err
	}
	pythonPackages, err := c.capturePythonPackages(ctx, inst, baseRelease)
	if err != nil {
		return Result{}, err
	}
	gitComponents, err := c.captureGitComponents(ctx, inst, baseRelease, baseManifest)
	if err != nil {
		return Result{}, err
	}

	// Carry container components over from the base unchanged (ghcr probing is
	// handled separately). Normalize to a non-nil map so the rendered manifest
	// emits an object rather than a JSON null.
	containerComponents := baseManifest.ContainerComponents
	if containerComponents == nil {
		containerComponents = map[string]manifest.ContainerComponent{}
	}

	captured := &manifest.Manifest{
		Release:             release,
		Description:         fmt.Sprintf("Local-only Tenstorrent stack snapshot %s, captured from %s", release, baseRelease),
		Components:          buildComponents(baseManifest, systemPackages, pythonPackages),
		SystemPackages:      systemPackages,
		PythonPackages:      pythonPackages,
		GitComponents:       gitComponents,
		ContainerComponents: containerComponents,
	}
	if err := captured.Validate(); err != nil {
		return Result{}, fmt.Errorf("capture: captured manifest is invalid: %w", err)
	}

	rendered, err := json.MarshalIndent(captured, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("capture: render manifest: %w", err)
	}
	rendered = append(rendered, '\n')

	result := Result{Release: release, BaseRelease: baseRelease, Path: target, ManifestJSON: rendered}
	if opts.DryRun {
		return result, nil
	}

	if err := writeManifestAtomically(target, rendered); err != nil {
		return Result{}, err
	}
	result.Written = true
	c.logf("Captured local release manifest: %s", target)
	return result, nil
}

// resolveBase determines the base release and loads its manifest. The base must
// be installed so its virtualenv and git clones can be probed.
func (c *Capturer) resolveBase(release, requested string, inst *version.Installer) (string, *manifest.Manifest, error) {
	base := requested
	if base == "" {
		latest, err := c.latestInstalledBase(release, inst)
		if err != nil {
			return "", nil, err
		}
		base = latest
	} else if err := version.ValidateRelease(base); err != nil {
		return "", nil, err
	}

	if !inst.IsInstalled(base) {
		return "", nil, fmt.Errorf("capture: base release %s is not installed at %s; capture reads installed versions", base, inst.ReleaseDir(base))
	}

	baseManifest, err := manifest.Load(filepath.Join(c.Root, "releases", base+".json"))
	if err != nil {
		return "", nil, fmt.Errorf("capture: load base manifest: %w", err)
	}
	return base, baseManifest, nil
}

// latestInstalledBase returns the lexicographically latest dated release that is
// installed and is not the capture target. Dated names sort chronologically.
func (c *Capturer) latestInstalledBase(release string, inst *version.Installer) (string, error) {
	entries, err := os.ReadDir(filepath.Join(c.Root, "releases"))
	if err != nil {
		return "", fmt.Errorf("capture: read releases directory: %w", err)
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if name == release || !datedReleaseRe.MatchString(name) {
			continue
		}
		if inst.IsInstalled(name) {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("capture: no installed dated base release found; install one or pass --base")
	}
	sort.Strings(candidates)
	return candidates[len(candidates)-1], nil
}

// resolveOSManifest detects the host OS, loads the matching OS manifest, and
// requires an apt-based manager (the only manager capture supports).
func (c *Capturer) resolveOSManifest() (*manifest.OSManifest, error) {
	osInfo, err := manifest.DetectOS(c.OSReleasePath)
	if err != nil {
		return nil, err
	}
	manifestsDir := filepath.Join(c.Root, "manifests")
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		return nil, fmt.Errorf("capture: read OS manifests directory: %w", err)
	}
	var available []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), osManifestSuffix) {
			available = append(available, strings.TrimSuffix(entry.Name(), osManifestSuffix))
		}
	}
	key, err := osInfo.ResolveManifestKey(available)
	if err != nil {
		return nil, err
	}
	osm, err := manifest.LoadOSManifest(filepath.Join(manifestsDir, key+osManifestSuffix))
	if err != nil {
		return nil, err
	}
	if osm.PackageManager() != "apt" {
		return nil, fmt.Errorf("capture: only apt OS manifests are supported, got %q", osm.PackageManager())
	}
	return osm, nil
}

// captureSystemPackages probes the installed version of each pinned and optional
// virtual package. A missing pinned package is a hard error; a missing optional
// package is omitted (not inherited from the base, which would misrepresent the
// installed state).
func (c *Capturer) captureSystemPackages(ctx context.Context, osm *manifest.OSManifest, base *manifest.Manifest) (map[string]string, error) {
	out := make(map[string]string)
	for _, virtual := range stackpolicy.CaptureVirtualPackages {
		name, ok := osm.ResolvePackage(virtual)
		if !ok {
			if stackpolicy.IsOptionalVirtualPackage(virtual) {
				continue
			}
			return nil, fmt.Errorf("capture: failed to resolve system package from OS manifest: %q", virtual)
		}
		ver, installed, err := c.dpkgVersion(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("capture: probe system package %q: %w", name, err)
		}
		if !installed {
			if stackpolicy.IsOptionalVirtualPackage(virtual) {
				c.logf("Optional system package %s (%s) is not installed; omitting from capture", virtual, name)
				continue
			}
			return nil, fmt.Errorf("capture: required system package %q (%s) is not installed", virtual, name)
		}
		out[virtual] = ver
	}
	return out, nil
}

// capturePythonPackages probes the installed version of each pip package within
// the base release virtualenv. Every pip package must be installed.
func (c *Capturer) capturePythonPackages(ctx context.Context, inst *version.Installer, baseRelease string) (map[string]string, error) {
	venvPython := filepath.Join(inst.ReleaseDir(baseRelease), "venv", "bin", "python")
	if _, err := os.Stat(venvPython); err != nil {
		return nil, fmt.Errorf("capture: base release %s has no virtualenv python at %s: %w", baseRelease, venvPython, err)
	}
	out := make(map[string]string, len(stackpolicy.PipPackages))
	for _, pkg := range stackpolicy.PipPackages {
		ver, installed, err := c.pipShowVersion(ctx, venvPython, pkg)
		if err != nil {
			return nil, fmt.Errorf("capture: probe python package %q: %w", pkg, err)
		}
		if !installed {
			return nil, fmt.Errorf("capture: required python package %q is not installed in %s", pkg, venvPython)
		}
		out[pkg] = ver
	}
	return out, nil
}

// captureGitComponents records each base git component's URL and the local HEAD
// of its installed clone.
func (c *Capturer) captureGitComponents(ctx context.Context, inst *version.Installer, baseRelease string, base *manifest.Manifest) (map[string]manifest.GitComponent, error) {
	out := make(map[string]manifest.GitComponent, len(base.GitComponents))
	if len(base.GitComponents) == 0 {
		return out, nil
	}
	srcRoot := filepath.Join(inst.ReleaseDir(baseRelease), "src")
	names := make([]string, 0, len(base.GitComponents))
	for name := range base.GitComponents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		repoDir := filepath.Join(srcRoot, name)
		if _, err := os.Stat(repoDir); err != nil {
			return nil, fmt.Errorf("capture: git component %q is not installed at %s: %w", name, repoDir, err)
		}
		head, err := c.gitHead(ctx, repoDir)
		if err != nil {
			return nil, err
		}
		out[name] = manifest.GitComponent{URL: base.GitComponents[name].URL, Version: head}
	}
	return out, nil
}

// buildComponents seeds the components map from the base manifest and overrides
// the kmd/smi entries with the freshly probed versions, mirroring proto1's
// _capture_components while keeping the base tt-metal and firmware entries.
func buildComponents(base *manifest.Manifest, systemPackages, pythonPackages map[string]string) map[string]manifest.Component {
	out := make(map[string]manifest.Component, len(base.Components)+2)
	for name, comp := range base.Components {
		out[name] = comp
	}
	if kmd := systemPackages["kmd"]; kmd != "" {
		out["tt-kmd"] = manifest.Component{Version: "ttkmd-" + kmd}
	}
	if smi := pythonPackages["tt-smi"]; smi != "" {
		out["tt-smi"] = manifest.Component{Version: "v" + smi}
	}
	return out
}

// writeManifestAtomically writes data to target via a temp file in the same
// directory followed by a rename, so a reader never sees a partial manifest.
func writeManifestAtomically(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("capture: create releases directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".capture-*.json")
	if err != nil {
		return fmt.Errorf("capture: create temp manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("capture: write temp manifest: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("capture: chmod temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("capture: close temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("capture: write manifest %s: %w", target, err)
	}
	return nil
}
