package install

// The Tenstorrent stack policy below mirrors the constants kept in proto1's
// lib/package_manager.sh. The ordered virtual-package list, the pinned and
// optional subsets, and the pip-package list are domain knowledge tied to the
// stack, so they live with the orchestrator rather than the generic
// package-manager engine.

// systemVirtualPackages is the ordered list of virtual system packages that the
// installer resolves against the OS manifest.
var systemVirtualPackages = []string{
	"cmake",
	"ninja",
	"zlib",
	"kmd",
	"smi",
	"flash",
	"topology",
	"metalium",
}

// pinnedVirtualPackages must carry a version pin from the stack manifest; a
// missing pin is a hard error.
var pinnedVirtualPackages = map[string]bool{
	"kmd":      true,
	"smi":      true,
	"flash":    true,
	"topology": true,
}

// optionalVirtualPackages are skipped when the OS manifest does not define them
// or the stack manifest does not pin them.
var optionalVirtualPackages = map[string]bool{
	"metalium": true,
}

// pipPackages is the ordered list of Python packages installed into the release
// virtualenv. Each must be pinned in the stack manifest's python_packages.
var pipPackages = []string{
	"tt-smi",
	"tt-umd",
	"textual",
	"elasticsearch",
	"tt-burnin",
}
