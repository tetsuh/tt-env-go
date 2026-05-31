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
func resolveSystemPackages(osm *manifest.OSManifest, m *manifest.Manifest, latest bool) ([]packagemanager.Package, error) {
	var pkgs []packagemanager.Package
	for _, virtual := range systemVirtualPackages {
		concrete, ok := osm.ResolvePackage(virtual)
		if !ok {
			if optionalVirtualPackages[virtual] {
				continue
			}
			return nil, fmt.Errorf("install: failed to resolve system package from OS manifest: %q", virtual)
		}

		// In --latest mode versions are intentionally unpinned so apt/dnf
		// installs the candidate (latest) version; the exact installed version
		// is recorded afterwards by capturing the environment. Optional packages
		// still follow the base manifest structure: an optional package that the
		// base does not declare is omitted so --latest does not pull in
		// unintended system dependencies.
		if latest {
			if optionalVirtualPackages[virtual] {
				if _, declared := m.SystemPackages[virtual]; !declared {
					continue
				}
			}
			pkgs = append(pkgs, packagemanager.Package{Name: concrete})
			continue
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
// _package_manager_resolved_pip_packages. In latest mode the versions are left
// empty so the venv provisioner installs them unpinned.
func resolvePipPackages(m *manifest.Manifest, latest bool) (map[string]string, error) {
	out := make(map[string]string, len(pipPackages))
	for _, name := range pipPackages {
		if latest {
			out[name] = ""
			continue
		}
		version := m.PythonPackages[name]
		if version == "" {
			return nil, fmt.Errorf("install: stack manifest missing python package version: python_packages.%s", name)
		}
		out[name] = version
	}
	return out, nil
}
