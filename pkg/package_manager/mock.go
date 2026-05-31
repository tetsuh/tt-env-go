package packagemanager

import (
	"context"
	"fmt"
	"strings"
)

// MockPackageManager is a PackageManager implementation for tests. It records
// each operation in Calls (in order) and lets tests program the installed state
// and per-operation errors. It is safe to use the zero value.
type MockPackageManager struct {
	// Calls is the ordered log of operations, e.g. "update",
	// "add-repo:tenstorrent", "install:kmd=2.8.0,smi", "remove:foo",
	// "is-installed:bar".
	Calls []string
	// Installed seeds and records the package installed state consulted by
	// IsInstalled and updated by Install/Remove.
	Installed map[string]bool
	// Errors maps an operation verb ("update", "add-repo", "install",
	// "remove", "is-installed") to an error to return for that operation.
	Errors map[string]error
}

var _ PackageManager = (*MockPackageManager)(nil)

func (m *MockPackageManager) record(call string) {
	m.Calls = append(m.Calls, call)
}

func (m *MockPackageManager) err(verb string) error {
	if m.Errors == nil {
		return nil
	}
	return m.Errors[verb]
}

// Update records an "update" call.
func (m *MockPackageManager) Update(_ context.Context) error {
	m.record("update")
	return m.err("update")
}

// AddRepo records an "add-repo:<name>" call.
func (m *MockPackageManager) AddRepo(_ context.Context, repo Repository) error {
	m.record("add-repo:" + repo.Name)
	return m.err("add-repo")
}

// Install records an "install:<pkgs>" call and, on success, marks the packages
// as installed. Pinned packages are recorded as name=version.
func (m *MockPackageManager) Install(_ context.Context, pkgs ...Package) error {
	specs := make([]string, len(pkgs))
	for i, p := range pkgs {
		if p.Version == "" {
			specs[i] = p.Name
		} else {
			specs[i] = p.Name + "=" + p.Version
		}
	}
	m.record("install:" + strings.Join(specs, ","))
	if err := m.err("install"); err != nil {
		return err
	}
	if m.Installed == nil {
		m.Installed = make(map[string]bool)
	}
	for _, p := range pkgs {
		m.Installed[p.Name] = true
	}
	return nil
}

// Remove records a "remove:<names>" call and, on success, marks the packages as
// not installed.
func (m *MockPackageManager) Remove(_ context.Context, names ...string) error {
	m.record("remove:" + strings.Join(names, ","))
	if err := m.err("remove"); err != nil {
		return err
	}
	for _, name := range names {
		if m.Installed != nil {
			delete(m.Installed, name)
		}
	}
	return nil
}

// IsInstalled records an "is-installed:<name>" call and reports whether the
// package is currently marked installed.
func (m *MockPackageManager) IsInstalled(_ context.Context, name string) (bool, error) {
	m.record("is-installed:" + name)
	if err := m.err("is-installed"); err != nil {
		return false, err
	}
	return m.Installed[name], nil
}

// String renders the recorded calls as a single newline-separated string, which
// is convenient for failure messages.
func (m *MockPackageManager) String() string {
	return fmt.Sprintf("%v", m.Calls)
}
