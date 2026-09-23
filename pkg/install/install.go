// Package install orchestrates installation of a Tenstorrent stack release. It
// drives the existing package-manager, venv, git-clone, shim, and version
// engines to reproduce proto1's install_release: resolve the stack and OS
// manifests, install system and Python packages, clone git components, write
// bin/<component> wrappers for git and container components, and generate shims.
//
// All filesystem staging is performed through version.Installer.Install, which
// stages into a partial directory and atomically promotes it. System package
// installation and repository configuration are system-wide side effects that
// happen during staging but outside the staged directory; like proto1, they are
// not rolled back if a later staging step fails (apt/dnf operations are
// idempotent).
package install

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tetsuh/tt-env-go/pkg/capture"
	"github.com/tetsuh/tt-env-go/pkg/catalog"
	"github.com/tetsuh/tt-env-go/pkg/gitclone"
	"github.com/tetsuh/tt-env-go/pkg/lock"
	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
	"github.com/tetsuh/tt-env-go/pkg/shims"
	"github.com/tetsuh/tt-env-go/pkg/update"
	"github.com/tetsuh/tt-env-go/pkg/venv"
	"github.com/tetsuh/tt-env-go/pkg/version"
)

// osManifestSuffix is the file extension of OS manifests under Root/manifests.
const osManifestSuffix = ".env"

// Orchestrator installs releases under a TT_HOME root directory.
type Orchestrator struct {
	// Root is the TT_HOME directory under which releases/, manifests/, and
	// versions/ live.
	Root string
	// Runner executes package-manager, venv, and git commands. When nil, the
	// engines default to their exec runners.
	Runner packagemanager.CommandRunner
	// OSReleasePath overrides the os-release path used for OS detection. When
	// empty, manifest.DetectOS uses its default (/etc/os-release).
	OSReleasePath string
	// Logf logs informational progress. When nil, slog.Info is used.
	Logf func(format string, args ...any)
	// LookSystemCommand resolves a command name to an absolute system path when
	// creating bin links. When nil, the preferred system directories are
	// searched. It exists primarily to make bin-link creation testable.
	LookSystemCommand func(command string) (string, bool)
	// Now supplies the install timestamp recorded in the lock; when nil,
	// time.Now is used.
	Now func() time.Time
	// DpkgVersion and PipShowVersion probe installed versions to resolve
	// unpinned or --latest packages in the lock. When nil, the runner-backed
	// capture probes are used. They exist primarily to make lock resolution
	// testable.
	DpkgVersion    func(ctx context.Context, name string) (string, bool, error)
	PipShowVersion func(ctx context.Context, venvPython, pkg string) (string, bool, error)
}

func (o *Orchestrator) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
		return
	}
	slog.Info(fmt.Sprintf(format, args...))
}

// Options configures an install.
type Options struct {
	// DryRun resolves and logs the planned actions without staging anything.
	DryRun bool
	// Force reinstalls an already installed release.
	Force bool
	// Latest installs the latest available versions (unpinned system/pip
	// packages, git components at their remote HEAD) instead of the pinned
	// versions in the manifest. Base supplies the release structure.
	Latest bool
	// Base names the release manifest whose structure (packages, repos, git and
	// container components) seeds a --latest install. When empty, the release
	// being installed is used as its own template.
	Base string
}

// plan captures the resolved actions for an install, built once from the stack
// and OS manifests so the dry-run and real paths share the same logic.
type plan struct {
	osManifestKey  string
	pkgManager     string
	useSystem      bool
	requiredRepos  []string
	systemPackages []packagemanager.Package
	pipPackages    map[string]string
	gitComponents  map[string]gitclone.Component
	containerRefs  map[string]string // component -> image reference
	// components is the full set of stack components, keyed by name, used by the
	// download path when USE_SYSTEM_PACKAGES=false.
	components map[string]manifest.Component
	// managedCommandNames is the set of command names already provided by git or
	// container component wrappers; they are excluded from system bin links.
	managedCommandNames map[string]bool
	// latest reports whether this is a --latest install: system and pip packages
	// are installed unpinned and git components are pinned to their remote HEAD.
	latest bool
	// Lock provenance, recorded in versions/<release>/manifest.json.
	release     string
	source      string // lock.SourceCatalog | lock.SourceLocal | lock.SourceLatest
	base        string // structure base for a --latest install
	catalogRepo string
	catalogRef  string
	// m is the manifest the plan was resolved from (intent), and osm the
	// resolved OS manifest; both feed the lock.
	m   *manifest.Manifest
	osm *manifest.OSManifest
}

// Install installs the named release. When opts.DryRun is true it resolves and
// logs the planned actions without staging anything. When opts.Force is true an
// already installed release is reinstalled. When opts.Latest is true the latest
// available versions are installed using opts.Base for the release structure.
func (o *Orchestrator) Install(ctx context.Context, release string, opts Options) (version.Result, error) {
	if err := version.ValidateRelease(release); err != nil {
		return version.Result{}, err
	}

	m, fromLocal, err := o.loadPlanManifest(release, opts)
	if err != nil {
		return version.Result{}, err
	}

	inst := &version.Installer{Root: o.Root}

	// A --latest install would otherwise silently no-op on an already installed
	// release; require --force so refreshing to latest versions is explicit.
	if opts.Latest && !opts.DryRun && !opts.Force && inst.IsInstalled(release) {
		return version.Result{}, fmt.Errorf("install: release %s is already installed; pass --force to refresh it to the latest versions", release)
	}

	if opts.DryRun {
		p, err := o.buildPlan(ctx, m, opts, release, fromLocal)
		if err != nil {
			return version.Result{}, err
		}
		o.logDryRun(release, p)
		return version.Result{Release: release, Path: inst.ReleaseDir(release)}, nil
	}

	var instOpts []version.Option
	if opts.Force {
		instOpts = append(instOpts, version.WithForce(true))
	}

	res, err := inst.Install(release, func(stagingDir string) error {
		p, err := o.buildPlan(ctx, m, opts, release, fromLocal)
		if err != nil {
			return err
		}
		return o.stage(ctx, stagingDir, p)
	}, instOpts...)
	if err != nil {
		return version.Result{}, err
	}

	if res.Installed {
		o.logf("Installed release %s at %s", release, res.Path)
	} else {
		o.logf("Release %s is already installed at %s", release, res.Path)
	}

	// proto1 regenerates shims both after a fresh install and on the
	// already-installed no-op path.
	if _, err := (&shims.Generator{Home: o.Root}).Generate(); err != nil {
		return res, fmt.Errorf("install: release %s installed but shim generation failed: %w", release, err)
	}

	return res, nil
}

// loadPlanManifest loads the manifest that supplies the install plan. For a
// normal install this is the release's own manifest; for a --latest install it
// is opts.Base (defaulting to the release), used only for its structure.
// Both lookups search the local manifest directory first (local overrides
// catalog). The second return value reports whether the manifest came from
// releases.local/ (false means the catalog cache), which the lock records as
// provenance.
func (o *Orchestrator) loadPlanManifest(release string, opts Options) (*manifest.Manifest, bool, error) {
	name := release
	kind := "release"
	if opts.Latest {
		base := opts.Base
		if base == "" {
			base = release
		} else if err := version.ValidateRelease(base); err != nil {
			return nil, false, err
		}
		name, kind = base, "base"
	}

	manifestPath, fromLocal, err := catalog.Path(o.Root, name)
	if err != nil {
		return nil, false, fmt.Errorf("install: resolve %s manifest: %w", kind, err)
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, false, err
	}
	if m.Release != name {
		return nil, false, fmt.Errorf("install: %s manifest %s declares %q, expected %q", kind, manifestPath, m.Release, name)
	}
	return m, fromLocal, nil
}

// buildPlan resolves the OS manifest and the concrete system/pip packages, git
// components, and container wrappers for the release. When opts.Latest is
// true, system and pip packages are installed unpinned and git components are
// pinned to their remote HEAD. It also records the lock provenance: the
// release being installed, where the plan manifest came from, and, for a
// --latest install, its structural base.
func (o *Orchestrator) buildPlan(ctx context.Context, m *manifest.Manifest, opts Options, release string, fromLocal bool) (*plan, error) {
	p, err := o.newPlan(m, opts, release, fromLocal)
	if err != nil {
		return nil, err
	}
	if err := o.resolvePlanPackages(p); err != nil {
		return nil, err
	}
	if err := o.resolvePlanGit(ctx, p); err != nil {
		return nil, err
	}
	if err := resolvePlanContainers(p); err != nil {
		return nil, err
	}
	if err := o.finalizePlan(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (o *Orchestrator) newPlan(m *manifest.Manifest, opts Options, release string, fromLocal bool) (*plan, error) {
	osm, key, err := o.resolveOSManifest()
	if err != nil {
		return nil, err
	}
	pkgManager := osm.PackageManager()
	if pkgManager == "" {
		return nil, fmt.Errorf("install: OS manifest %s does not define PKG_MANAGER", key)
	}

	p := &plan{
		osManifestKey: key,
		pkgManager:    pkgManager,
		useSystem:     osm.UseSystemPackages(),
		requiredRepos: osm.RequiredRepos(),
		latest:        opts.Latest,
		release:       release,
		m:             m,
		osm:           osm,
	}
	if opts.Latest {
		p.source = lock.SourceLatest
		p.base = release
		if opts.Base != "" {
			p.base = opts.Base
		}
	} else if fromLocal {
		p.source = lock.SourceLocal
	} else {
		p.source = lock.SourceCatalog
	}
	if !fromLocal {
		// Best-effort: cite the catalog repository and ref the plan manifest
		// was fetched from. A cache that predates the provenance record simply
		// omits it.
		if src, ok, err := update.ReadCatalogSource(o.Root); err == nil && ok {
			p.catalogRepo, p.catalogRef = src.Repo, src.Ref
		}
	}
	return p, nil
}

func (o *Orchestrator) resolvePlanPackages(p *plan) error {
	if !p.useSystem {
		return nil
	}
	var err error
	p.systemPackages, err = resolveSystemPackages(p.osm, p.m, p.latest)
	if err != nil {
		return err
	}
	p.pipPackages, err = resolvePipPackages(p.m, p.latest)
	return err
}

func (o *Orchestrator) resolvePlanGit(ctx context.Context, p *plan) error {
	p.gitComponents = make(map[string]gitclone.Component, len(p.m.GitComponents))
	for name, gc := range p.m.GitComponents {
		if err := validateComponentName(name); err != nil {
			return err
		}
		version := gc.Version
		if p.latest {
			resolved, err := (&gitclone.Cloner{Runner: o.Runner}).ResolveHead(ctx, gc.URL)
			if err != nil {
				return fmt.Errorf("install: resolve latest revision for git component %q: %w", name, err)
			}
			version = resolved
		}
		p.gitComponents[name] = gitclone.Component{URL: gc.URL, Version: version}
	}
	return nil
}

func resolvePlanContainers(p *plan) error {
	p.containerRefs = make(map[string]string)
	for name, cc := range p.m.ContainerComponents {
		// Ref-only components are capture/diff metadata; proto1's install only
		// wraps components that declare an image URL.
		if cc.ImageURL == "" {
			continue
		}
		if err := validateComponentName(name); err != nil {
			return err
		}
		ref := containerImageRef(cc.ImageURL, cc.ImageTag)
		if err := validateImageRef(ref); err != nil {
			return err
		}
		p.containerRefs[name] = ref
	}
	return nil
}

func (o *Orchestrator) finalizePlan(p *plan) error {
	p.components = p.m.Components
	p.managedCommandNames = make(map[string]bool, len(p.gitComponents)+len(p.containerRefs))
	for name := range p.gitComponents {
		p.managedCommandNames[name] = true
	}
	for name := range p.containerRefs {
		p.managedCommandNames[name] = true
	}

	// When system packages are disabled the install relies entirely on the
	// download path, so validate the component download metadata up front; this
	// lets the dry-run surface problems a real install would hit.
	if !p.useSystem {
		return validateDownloadComponents(p.components)
	}
	return nil
}

// stage performs the install work into stagingDir and records the resolved
// lock manifest, so the promoted release tree is self-describing (issue #78):
// the lock is written before promotion and lands atomically with the rest of
// the release.
func (o *Orchestrator) stage(ctx context.Context, stagingDir string, p *plan) error {
	if p.useSystem {
		if err := o.installSystemPackages(ctx, p); err != nil {
			return err
		}
		if err := o.provisionVenv(ctx, stagingDir, p); err != nil {
			return err
		}
		if err := o.createSystemBinLinks(stagingDir, p); err != nil {
			return err
		}
	} else {
		if err := o.downloadComponents(ctx, stagingDir, p.components); err != nil {
			return err
		}
	}

	if err := o.installGitComponents(ctx, stagingDir, p); err != nil {
		return err
	}
	if err := o.installContainerComponents(stagingDir, p); err != nil {
		return err
	}
	return o.writeLock(ctx, stagingDir, p)
}

// writeLock resolves the concrete versions that were just installed and writes
// them, with provenance, to stagingDir/manifest.json. Pinned entries record
// their pins (apt/dnf and pip install exactly those); unpinned and --latest
// entries are probed after installation, reusing the capture engine's probes.
func (o *Orchestrator) writeLock(ctx context.Context, stagingDir string, p *plan) error {
	systemPackages, err := o.resolvedSystemPackages(ctx, p)
	if err != nil {
		return err
	}
	pythonPackages, err := o.resolvedPipPackages(ctx, stagingDir, p)
	if err != nil {
		return err
	}

	gitComponents := make(map[string]manifest.GitComponent, len(p.gitComponents))
	for name, gc := range p.gitComponents {
		gitComponents[name] = manifest.GitComponent{URL: gc.URL, Version: gc.Version}
	}

	l := &lock.Lock{
		Manifest: manifest.Manifest{
			Release:             p.release,
			Description:         p.m.Description,
			Components:          capture.BuildComponents(p.m, systemPackages, pythonPackages),
			SystemPackages:      systemPackages,
			PythonPackages:      pythonPackages,
			GitComponents:       gitComponents,
			ContainerComponents: p.m.ContainerComponents,
		},
		Source:      p.source,
		Base:        p.base,
		CatalogRepo: p.catalogRepo,
		CatalogRef:  p.catalogRef,
		InstalledAt: o.now().UTC(),
	}
	if err := lock.Write(stagingDir, l); err != nil {
		return fmt.Errorf("install: record lock for release %s: %w", p.release, err)
	}
	o.logf("Recorded resolved versions in %s", lock.Path(stagingDir))
	return nil
}

// resolvedSystemPackages maps the virtual system package names to the versions
// that were installed: the pin when the plan carried one, otherwise the
// probed installed version (unpinned and --latest installs).
func (o *Orchestrator) resolvedSystemPackages(ctx context.Context, p *plan) (map[string]string, error) {
	out := make(map[string]string, len(p.systemPackages))
	for _, pkg := range p.systemPackages {
		virtual := virtualForConcrete(p.osm, pkg.Name)
		if virtual == "" {
			continue
		}
		version := pkg.Version
		if version == "" {
			ver, installed, err := o.systemPackageVersion(ctx, p.pkgManager, pkg.Name)
			if err != nil {
				return nil, fmt.Errorf("install: probe installed version of %q for the lock: %w", pkg.Name, err)
			}
			if !installed {
				return nil, fmt.Errorf("install: system package %q (%s) is not installed after the install step", virtual, pkg.Name)
			}
			version = ver
		}
		out[virtual] = version
	}
	return out, nil
}

// resolvedPipPackages maps the pip package names to the versions that were
// installed: the pin when the plan carried one, otherwise the version probed
// from the staging virtualenv (--latest installs).
func (o *Orchestrator) resolvedPipPackages(ctx context.Context, stagingDir string, p *plan) (map[string]string, error) {
	out := make(map[string]string, len(p.pipPackages))
	venvPython := filepath.Join(stagingDir, "venv", "bin", "python")
	for name, pinned := range p.pipPackages {
		version := pinned
		if version == "" {
			ver, installed, err := o.pipShowVersion(ctx, venvPython, name)
			if err != nil {
				return nil, fmt.Errorf("install: probe installed version of python package %q for the lock: %w", name, err)
			}
			if !installed {
				return nil, fmt.Errorf("install: python package %q is not installed in the staging virtualenv", name)
			}
			version = ver
		}
		out[name] = version
	}
	return out, nil
}

// virtualForConcrete maps a concrete package name back to the virtual system
// package name it was resolved from, or "" when the OS manifest does not
// define it.
func virtualForConcrete(osm *manifest.OSManifest, concrete string) string {
	for _, virtual := range systemVirtualPackages {
		if name, ok := osm.ResolvePackage(virtual); ok && name == concrete {
			return virtual
		}
	}
	return ""
}

func (o *Orchestrator) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// systemPackageVersion probes an unpinned package with the query tool native
// to the selected package manager. DpkgVersion remains injectable for apt; RPM
// packages use the same rpm query implementation as the capture probes.
func (o *Orchestrator) systemPackageVersion(ctx context.Context, manager, name string) (string, bool, error) {
	switch manager {
	case "apt":
		return o.dpkgVersion(ctx, name)
	case "dnf":
		return capture.RpmQuery(ctx, o.Runner, name)
	default:
		return "", false, fmt.Errorf("install: cannot probe package version for unsupported package manager %q", manager)
	}
}

func (o *Orchestrator) dpkgVersion(ctx context.Context, name string) (string, bool, error) {
	if o.DpkgVersion != nil {
		return o.DpkgVersion(ctx, name)
	}
	return capture.DpkgQuery(ctx, o.Runner, name)
}

func (o *Orchestrator) pipShowVersion(ctx context.Context, venvPython, pkg string) (string, bool, error) {
	if o.PipShowVersion != nil {
		return o.PipShowVersion(ctx, venvPython, pkg)
	}
	return capture.PipShow(ctx, o.Runner, venvPython, pkg)
}

// provisionVenv creates the staging virtualenv and installs the resolved pip
// packages. In latest mode the packages are installed unpinned so pip selects
// the newest compatible versions.
func (o *Orchestrator) provisionVenv(ctx context.Context, stagingDir string, p *plan) error {
	prov := &venv.Provisioner{Runner: o.Runner}
	if p.latest {
		names := make([]string, 0, len(p.pipPackages))
		for name := range p.pipPackages {
			names = append(names, name)
		}
		sort.Strings(names)
		if err := prov.ProvisionLatest(ctx, stagingDir, names); err != nil {
			return fmt.Errorf("install: provision virtualenv: %w", err)
		}
		return nil
	}
	if err := prov.Provision(ctx, stagingDir, p.pipPackages); err != nil {
		return fmt.Errorf("install: provision virtualenv: %w", err)
	}
	return nil
}

// installSystemPackages configures required repositories and installs the
// resolved system packages.
func (o *Orchestrator) installSystemPackages(ctx context.Context, p *plan) error {
	mgr, err := o.packageManager(p.pkgManager)
	if err != nil {
		return err
	}
	for _, repo := range p.requiredRepos {
		if err := mgr.AddRepo(ctx, packagemanager.Repository{Name: repo, URI: repo}); err != nil {
			return fmt.Errorf("install: add repository %q: %w", repo, err)
		}
	}
	if err := mgr.Update(ctx); err != nil {
		return fmt.Errorf("install: update package metadata: %w", err)
	}
	if err := mgr.Install(ctx, p.systemPackages...); err != nil {
		return fmt.Errorf("install: install system packages: %w", err)
	}
	return nil
}

// installGitComponents clones git components into stagingDir/src, verifies each
// entrypoint, and writes a bin/<component> wrapper.
func (o *Orchestrator) installGitComponents(ctx context.Context, stagingDir string, p *plan) error {
	if len(p.gitComponents) == 0 {
		return nil
	}
	srcDir := filepath.Join(stagingDir, "src")
	cloner := &gitclone.Cloner{Runner: o.Runner}
	if err := cloner.Provision(ctx, srcDir, p.gitComponents); err != nil {
		return fmt.Errorf("install: clone git components: %w", err)
	}

	binDir := filepath.Join(stagingDir, "bin")
	for name := range p.gitComponents {
		entrypoint := defaultEntrypoint
		entrypointPath := filepath.Join(srcDir, name, entrypoint)
		if _, err := os.Stat(entrypointPath); err != nil {
			return fmt.Errorf("install: entrypoint %s not found for git component %q: %w", entrypoint, name, err)
		}
		content, err := renderGitWrapper(name, entrypoint, venv.DefaultSubdir)
		if err != nil {
			return err
		}
		if err := writeWrapper(binDir, name, content); err != nil {
			return err
		}
		o.logf("Created wrapper for git component %s", name)
	}
	return nil
}

// installContainerComponents writes a bin/<component> docker-run wrapper for
// each container component that declares an image.
func (o *Orchestrator) installContainerComponents(stagingDir string, p *plan) error {
	if len(p.containerRefs) == 0 {
		return nil
	}
	binDir := filepath.Join(stagingDir, "bin")
	for name, ref := range p.containerRefs {
		content, err := renderContainerWrapper(name, ref)
		if err != nil {
			return err
		}
		if err := writeWrapper(binDir, name, content); err != nil {
			return err
		}
		o.logf("Created wrapper for container component %s using image %s", name, ref)
	}
	return nil
}

// packageManager constructs the package-manager adapter for the named manager.
func (o *Orchestrator) packageManager(name string) (packagemanager.PackageManager, error) {
	switch name {
	case "apt":
		return packagemanager.NewAptManager(o.Runner), nil
	case "dnf":
		return packagemanager.NewDnfManager(o.Runner), nil
	default:
		return nil, fmt.Errorf("install: unsupported package manager: %q", name)
	}
}

// resolveOSManifest detects the host OS and loads the matching OS manifest from
// Root/manifests, returning the manifest and its suffixless key.
func (o *Orchestrator) resolveOSManifest() (*manifest.OSManifest, string, error) {
	osInfo, err := manifest.DetectOS(o.OSReleasePath)
	if err != nil {
		return nil, "", err
	}

	manifestsDir := filepath.Join(o.Root, "manifests")
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		return nil, "", fmt.Errorf("install: read OS manifests directory: %w", err)
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
		return nil, "", err
	}

	osm, err := manifest.LoadOSManifest(filepath.Join(manifestsDir, key+osManifestSuffix))
	if err != nil {
		return nil, "", err
	}
	return osm, key, nil
}

// logDryRun logs the planned actions without performing them.
func (o *Orchestrator) logDryRun(release string, p *plan) {
	mode := ""
	if p.latest {
		mode = " (latest available versions)"
	}
	o.logf("[dry-run] Would install release %s%s (OS manifest %s, package manager %s)", release, mode, p.osManifestKey, p.pkgManager)
	if p.useSystem {
		for _, repo := range p.requiredRepos {
			o.logf("[dry-run] Would add repository %s", repo)
		}
		for _, pkg := range p.systemPackages {
			if pkg.Version == "" {
				o.logf("[dry-run] Would install system package %s", pkg.Name)
			} else {
				o.logf("[dry-run] Would install system package %s (%s)", pkg.Name, pkg.Version)
			}
		}
		for name, ver := range p.pipPackages {
			if ver == "" {
				o.logf("[dry-run] Would install pip package %s", name)
			} else {
				o.logf("[dry-run] Would install pip package %s==%s", name, ver)
			}
		}
		o.logf("[dry-run] Would create system/venv bin links for installed commands")
	} else {
		for name := range p.components {
			o.logf("[dry-run] Would download component %s and verify its checksum", name)
		}
	}
	for name, gc := range p.gitComponents {
		o.logf("[dry-run] Would clone git component %s at %s and create its wrapper", name, gc.Version)
	}
	for name, ref := range p.containerRefs {
		o.logf("[dry-run] Would create container component wrapper for %s using image %s", name, ref)
	}
	o.logf("[dry-run] Would create version directory for %s", release)
	o.logf("[dry-run] Would record the resolved versions in %s", filepath.Join("versions", release, lock.FileName))
}
