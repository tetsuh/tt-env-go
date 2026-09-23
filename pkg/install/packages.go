package install

import (
	"fmt"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// resolveSystemPackages maps the ordered virtual system packages to concrete
// packages using the OS manifest, carrying version pins from the stack
// manifest. Optional packages are skipped when unresolved or unpinned; other
// resolved packages may be installed unpinned unless their manifest entry
// supplies a version.
func resolveSystemPackages(osm *manifest.OSManifest, m *manifest.Manifest, upgrade bool) ([]packagemanager.Package, error) {
	var pkgs []packagemanager.Package
	for _, virtual := range systemVirtualPackages {
		concrete, ok := osm.ResolvePackage(virtual)
		if !ok {
			if optionalVirtualPackages[virtual] {
				continue
			}
			return nil, fmt.Errorf("install: failed to resolve system package from OS manifest: %q", virtual)
		}

		// In upgrade mode apt/dnf selects current candidates, whose actual
		// versions are then recorded in the install lock. Optional packages
		// still follow the template: undeclared ones are not pulled in.
		if upgrade {
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
			// Unpinned package (cmake/ninja/zlib): install without a version.
		}

		pkgs = append(pkgs, packagemanager.Package{Name: concrete, Version: version})
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("install: no system packages resolved from OS manifest")
	}
	return pkgs, nil
}

// resolvePipPackages maps the ordered pip packages to versions from the stack
// manifest. Empty versions are resolved by pip and recorded in the lock. In
// upgrade mode all versions are deliberately empty.
func resolvePipPackages(m *manifest.Manifest, upgrade bool) (map[string]string, error) {
	out := make(map[string]string, len(pipPackages))
	for _, name := range pipPackages {
		if upgrade {
			out[name] = ""
			continue
		}
		out[name] = m.PythonPackages[name]
	}
	return out, nil
}
