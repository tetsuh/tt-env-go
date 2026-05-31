// Package stackpolicy holds the Tenstorrent stack package policy shared by the
// install and capture engines. The ordered virtual-package list, the pinned and
// optional subsets, and the pip-package list are domain knowledge tied to the
// stack (mirroring proto1's lib/package_manager.sh), so they live in one place
// to avoid drift between the engines that consume them.
package stackpolicy

// SystemVirtualPackages is the ordered list of virtual system packages the
// installer resolves against the OS manifest.
var SystemVirtualPackages = []string{
	"cmake",
	"ninja",
	"zlib",
	"kmd",
	"smi",
	"flash",
	"topology",
	"metalium",
}

// PinnedVirtualPackages must carry a version pin from the stack manifest; a
// missing pin is a hard error during install.
var PinnedVirtualPackages = map[string]bool{
	"kmd":      true,
	"smi":      true,
	"flash":    true,
	"topology": true,
}

// OptionalVirtualPackages are skipped when the OS manifest does not define them
// or the stack manifest does not pin them.
var OptionalVirtualPackages = map[string]bool{
	"metalium": true,
}

// CaptureVirtualPackages is the ordered list of virtual packages whose installed
// versions capture probes: the pinned set followed by the optional set. It omits
// the unpinned build dependencies (cmake/ninja/zlib), mirroring proto1's
// _capture_system_packages.
var CaptureVirtualPackages = []string{
	"kmd",
	"smi",
	"flash",
	"topology",
	"metalium",
}

// PipPackages is the ordered list of Python packages installed into the release
// virtualenv. Each must be pinned in the stack manifest's python_packages.
var PipPackages = []string{
	"tt-smi",
	"tt-umd",
	"textual",
	"elasticsearch",
	"tt-burnin",
}

// IsOptionalVirtualPackage reports whether the named virtual package is optional.
func IsOptionalVirtualPackage(virtual string) bool {
	return OptionalVirtualPackages[virtual]
}
