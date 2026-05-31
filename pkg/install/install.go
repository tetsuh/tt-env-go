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
}

func (o *Orchestrator) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
		return
	}
	slog.Info(fmt.Sprintf(format, args...))
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
}

// Install installs the named release. When dryRun is true it resolves and logs
// the planned actions without staging anything. When force is true an already
// installed release is reinstalled.
func (o *Orchestrator) Install(ctx context.Context, release string, dryRun, force bool) (version.Result, error) {
	if err := version.ValidateRelease(release); err != nil {
		return version.Result{}, err
	}

	manifestPath := filepath.Join(o.Root, "releases", release+".json")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return version.Result{}, err
	}
	if m.Release != release {
		return version.Result{}, fmt.Errorf("install: release manifest %s declares %q, expected %q", manifestPath, m.Release, release)
	}

	if dryRun {
		p, err := o.buildPlan(m)
		if err != nil {
			return version.Result{}, err
		}
		o.logDryRun(release, p)
		return version.Result{Release: release, Path: (&version.Installer{Root: o.Root}).ReleaseDir(release)}, nil
	}

	inst := &version.Installer{Root: o.Root}
	var opts []version.Option
	if force {
		opts = append(opts, version.WithForce(true))
	}

	res, err := inst.Install(release, func(stagingDir string) error {
		p, err := o.buildPlan(m)
		if err != nil {
			return err
		}
		return o.stage(ctx, stagingDir, p)
	}, opts...)
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

// buildPlan resolves the OS manifest and the concrete system/pip packages, git
// components, and container wrappers for the release.
func (o *Orchestrator) buildPlan(m *manifest.Manifest) (*plan, error) {
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
	}

	if p.useSystem {
		p.systemPackages, err = resolveSystemPackages(osm, m)
		if err != nil {
			return nil, err
		}
		p.pipPackages, err = resolvePipPackages(m)
		if err != nil {
			return nil, err
		}
	}

	p.gitComponents = make(map[string]gitclone.Component, len(m.GitComponents))
	for name, gc := range m.GitComponents {
		if err := validateComponentName(name); err != nil {
			return nil, err
		}
		p.gitComponents[name] = gitclone.Component{URL: gc.URL, Version: gc.Version}
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

	return p, nil
}

// stage performs the install work into stagingDir.
func (o *Orchestrator) stage(ctx context.Context, stagingDir string, p *plan) error {
	if p.useSystem {
		if err := o.installSystemPackages(ctx, p); err != nil {
			return err
		}
		if err := (&venv.Provisioner{Runner: o.Runner}).Provision(ctx, stagingDir, p.pipPackages); err != nil {
			return fmt.Errorf("install: provision virtualenv: %w", err)
		}
	} else {
		return fmt.Errorf("install: USE_SYSTEM_PACKAGES=false download path is not yet implemented (tracked in #58)")
	}

	if err := o.installGitComponents(ctx, stagingDir, p); err != nil {
		return err
	}
	return o.installContainerComponents(stagingDir, p)
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
	o.logf("[dry-run] Would install release %s (OS manifest %s, package manager %s)", release, p.osManifestKey, p.pkgManager)
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
			o.logf("[dry-run] Would install pip package %s==%s", name, ver)
		}
	} else {
		o.logf("[dry-run] Would download components (USE_SYSTEM_PACKAGES=false)")
	}
	for name := range p.gitComponents {
		o.logf("[dry-run] Would clone git component %s and create its wrapper", name)
	}
	for name, ref := range p.containerRefs {
		o.logf("[dry-run] Would create container component wrapper for %s using image %s", name, ref)
	}
	o.logf("[dry-run] Would create version directory for %s", release)
}
