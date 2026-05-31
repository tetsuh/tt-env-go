package install

import "github.com/tetsuh/tt-env-go/pkg/stackpolicy"

// The Tenstorrent stack package policy is defined in pkg/stackpolicy and shared
// with the capture engine. The aliases below keep the install code unchanged
// while sourcing the ordered virtual-package list, the pinned and optional
// subsets, and the pip-package list from the single shared definition.

var systemVirtualPackages = stackpolicy.SystemVirtualPackages

var pinnedVirtualPackages = stackpolicy.PinnedVirtualPackages

var optionalVirtualPackages = stackpolicy.OptionalVirtualPackages

var pipPackages = stackpolicy.PipPackages

// pipPackageCommands are the bin command names that are provided by pip
// packages. When such a command is found as a system command and a release
// virtualenv exists, the installer writes an absolute python wrapper rather than
// a symlink, mirroring proto1's TT_PACKAGE_MANAGER_PIP_PACKAGE_COMMANDS.
var pipPackageCommands = map[string]bool{
	"tt-smi":    true,
	"tt-burnin": true,
}

// preferredSystemCommandDirs is the ordered allowlist of directories searched
// for an existing system command when creating bin links. It mirrors proto1's
// TT_INSTALL_SYSTEM_COMMAND_DIRS default.
var preferredSystemCommandDirs = []string{
	"/usr/local/bin",
	"/usr/bin",
	"/bin",
	"/usr/local/sbin",
	"/usr/sbin",
	"/sbin",
}
