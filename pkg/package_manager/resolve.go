package packagemanager

import "fmt"

// VirtualResolver maps a virtual package name to a concrete package name. It is
// satisfied by *manifest.OSManifest, letting this package resolve virtual
// packages without importing the manifest package.
type VirtualResolver interface {
	ResolvePackage(virtual string) (string, bool)
}

// VirtualPackage names a virtual package to resolve together with an optional
// version pin. An empty Version leaves the resulting package unpinned.
type VirtualPackage struct {
	Name    string
	Version string
}

// ResolvePackages maps virtual packages to concrete Packages using resolver,
// carrying over any version pins. It returns an error if a virtual package is
// not defined by the resolver.
func ResolvePackages(resolver VirtualResolver, virtuals ...VirtualPackage) ([]Package, error) {
	pkgs := make([]Package, 0, len(virtuals))
	for _, v := range virtuals {
		concrete, ok := resolver.ResolvePackage(v.Name)
		if !ok {
			return nil, fmt.Errorf("virtual package not defined: %s", v.Name)
		}
		pkgs = append(pkgs, Package{Name: concrete, Version: v.Version})
	}
	return pkgs, nil
}
