// Package packagemanager defines the package-manager adapter contract used by
// the per-distro adapters (apt, dnf) together with a command-runner abstraction
// and mocks that allow adapters to be developed and tested without touching a
// real system package manager.
package packagemanager

import "context"

// Package is a system package to operate on. Version is an optional pin; an
// empty Version means the package is unpinned.
type Package struct {
	Name    string
	Version string
}

// Repository is the logical identity of a package repository that an adapter
// must configure before installing packages. It intentionally carries only a
// name and URI; distro-specific details (apt gpg keys, codename/suite,
// components, dnf repo files) are resolved by the concrete adapter from its
// injected OS manifest and configuration. Fields may be added later without
// breaking keyed struct literals.
type Repository struct {
	Name string
	URI  string
}

// PackageManager is the contract implemented by per-distro adapters. Adapters
// translate these operations into native package-manager commands.
type PackageManager interface {
	// Update refreshes the local package metadata.
	Update(ctx context.Context) error
	// AddRepo configures a package repository.
	AddRepo(ctx context.Context, repo Repository) error
	// Install installs the given packages, honoring any version pins.
	Install(ctx context.Context, pkgs ...Package) error
	// Remove removes the named packages.
	Remove(ctx context.Context, names ...string) error
	// IsInstalled reports whether the named package is installed.
	IsInstalled(ctx context.Context, name string) (bool, error)
}
