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

	"github.com/tetsuh/tt-env-go/pkg/gitclone"
	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
	"github.com/tetsuh/tt-env-go/pkg/shims"
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
}

// Install installs the named release. When opts.DryRun is true it resolves and
// logs the planned actions without staging anything. When opts.Force is true an
// already installed release is reinstalled. When opts.Latest is true the latest
// available versions are installed using opts.Base for the release structure.
func (o *Orchestrator) Install(ctx context.Context, release string, opts Options) (version.Result, error) {
	if err := version.ValidateRelease(release); err != nil {
		return version.Result{}, err
	}

	m, err := o.loadPlanManifest(release, opts)
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
		p, err := o.buildPlan(ctx, m, opts.Latest)
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
		p, err := o.buildPlan(ctx, m, opts.Latest)
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
func (o *Orchestrator) loadPlanManifest(release string, opts Options) (*manifest.Manifest, error) {
	if opts.Latest {
		base := opts.Base
		if base == "" {
			base = release
		} else if err := version.ValidateRelease(base); err != nil {
			return nil, err
		}
		manifestPath := filepath.Join(o.Root, "releases", base+".json")
		m, err := manifest.Load(manifestPath)
		if err != nil {
			return nil, err
		}
		if m.Release != base {
			return nil, fmt.Errorf("install: base manifest %s declares %q, expected %q", manifestPath, m.Release, base)
		}
		return m, nil
	}

	manifestPath := filepath.Join(o.Root, "releases", release+".json")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	if m.Release != release {
		return nil, fmt.Errorf("install: release manifest %s declares %q, expected %q", manifestPath, m.Release, release)
	}
	return m, nil
}

// buildPlan resolves the OS manifest and the concrete system/pip packages, git
// components, and container wrappers for the release. When latest is true,
// system and pip packages are installed unpinned and git components are pinned
// to their remote HEAD.
func (o *Orchestrator) buildPlan(ctx context.Context, m *manifest.Manifest, latest bool) (*plan, error) {
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
		latest:        latest,
	}

	if p.useSystem {
		p.systemPackages, err = resolveSystemPackages(osm, m, latest)
		if err != nil {
			return nil, err
		}
		p.pipPackages, err = resolvePipPackages(m, latest)
		if err != nil {
			return nil, err
		}
	}

	p.gitComponents = make(map[string]gitclone.Component, len(m.GitComponents))
	for name, gc := range m.GitComponents {
		if err := validateComponentName(name); err != nil {
			return nil, err
		}
		ver := gc.Version
		if latest {
			ver, err = (&gitclone.Cloner{Runner: o.Runner}).ResolveHead(ctx, gc.URL)
			if err != nil {
				return nil, fmt.Errorf("install: resolve latest revision for git component %q: %w", name, err)
			}
		}
		p.gitComponents[name] = gitclone.Component{URL: gc.URL, Version: ver}
	}

	p.containerRefs = make(map[string]string)
	for name, cc := range m.ContainerComponents {
		// Ref-only components are capture/diff metadata; proto1's install only
		// wraps components that declare an image URL.
		if cc.ImageURL == "" {
			continue
		}
		if err := validateComponentName(name); err != nil {
			return nil, err
		}
		ref := containerImageRef(cc.ImageURL, cc.ImageTag)
		if err := validateImageRef(ref); err != nil {
			return nil, err
		}
		p.containerRefs[name] = ref
	}

	p.components = m.Components
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
		if err := validateDownloadComponents(p.components); err != nil {
			return nil, err
		}
	}

	return p, nil
}

// stage performs the install work into stagingDir.
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
	return o.installContainerComponents(stagingDir, p)
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
}
