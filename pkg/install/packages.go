package install

import (
	"fmt"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// resolveSystemPackages maps the ordered virtual system packages to concrete
// packages using the OS manifest, carrying version pins from the stack
// manifest. It mirrors proto1's _package_manager_resolved_packages: optional
// packages are skipped when unresolved or unpinned, pinned packages require a
// version, and the remaining packages may be installed unpinned.
func resolveSystemPackages(osm *manifest.OSManifest, m *manifest.Manifest) ([]packagemanager.Package, error) {
	var pkgs []packagemanager.Package
	for _, virtual := range systemVirtualPackages {
		concrete, ok := osm.ResolvePackage(virtual)
		if !ok {
			if optionalVirtualPackages[virtual] {
				continue
			}
			return nil, fmt.Errorf("install: failed to resolve system package from OS manifest: %q", virtual)
		}

		version := m.SystemPackages[virtual]
		if version == "" {
			if optionalVirtualPackages[virtual] {
				continue
			}
			if pinnedVirtualPackages[virtual] {
				return nil, fmt.Errorf("install: stack manifest missing system package version: system_packages.%s", virtual)
			}
			// Unpinned package (cmake/ninja/zlib): install without a version.
		}

		pkgs = append(pkgs, packagemanager.Package{Name: concrete, Version: version})
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("install: no system packages resolved from OS manifest")
	}
	return pkgs, nil
}

// resolvePipPackages maps the ordered pip packages to their pinned versions from
// the stack manifest. Every pip package must be pinned, mirroring proto1's
// _package_manager_resolved_pip_packages.
func resolvePipPackages(m *manifest.Manifest) (map[string]string, error) {
	out := make(map[string]string, len(pipPackages))
	for _, name := range pipPackages {
		version := m.PythonPackages[name]
		if version == "" {
			return nil, fmt.Errorf("install: stack manifest missing python package version: python_packages.%s", name)
		}
		out[name] = version
	}
	return out, nil
}
