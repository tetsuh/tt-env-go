package kmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// DefaultSysModuleDir is the sysfs directory that exposes loaded kernel
// modules. A module is considered loaded when its subdirectory exists.
const DefaultSysModuleDir = "/sys/module"

// DefaultModinfo is the executable queried for a module's version.
const DefaultModinfo = "modinfo"

// ModuleVersion describes whether the Tenstorrent kernel module is loaded and,
// if so, the version reported by modinfo.
type ModuleVersion struct {
	// Loaded reports whether the module is currently loaded.
	Loaded bool
	// Version is the module version. It is empty when the module is not loaded
	// or its version could not be determined.
	Version string
}

// VersionProber reports the loaded state and version of a kernel module,
// mirroring proto1 lib/status.sh _status_kmd_version.
type VersionProber struct {
	// Runner executes modinfo. If nil, ExecRunner is used.
	Runner packagemanager.CommandRunner
	// Module is the module name. If empty, DefaultModule is used.
	Module string
	// SysModuleDir is probed to decide whether the module is loaded. If empty,
	// DefaultSysModuleDir is used.
	SysModuleDir string
	// Modinfo is the executable invoked for the version. If empty,
	// DefaultModinfo is used.
	Modinfo string
}

// EnvModule overrides the probed module name (proto1 TT_STATUS_KMD_MODULE).
const EnvModule = "TT_STATUS_KMD_MODULE"

// EnvSysModuleDir overrides the probed sysfs module directory
// (proto1 TT_STATUS_SYS_MODULE_DIR).
const EnvSysModuleDir = "TT_STATUS_SYS_MODULE_DIR"

func (p *VersionProber) runner() packagemanager.CommandRunner {
	if p.Runner != nil {
		return p.Runner
	}
	return packagemanager.ExecRunner{}
}

func (p *VersionProber) module() string {
	if p.Module != "" {
		return p.Module
	}
	if env := strings.TrimSpace(os.Getenv(EnvModule)); env != "" {
		return env
	}
	return DefaultModule
}

func (p *VersionProber) sysModuleDir() string {
	if p.SysModuleDir != "" {
		return p.SysModuleDir
	}
	if env := strings.TrimSpace(os.Getenv(EnvSysModuleDir)); env != "" {
		return env
	}
	return DefaultSysModuleDir
}

func (p *VersionProber) modinfo() string {
	if p.Modinfo != "" {
		return p.Modinfo
	}
	return DefaultModinfo
}

// Probe reports the module's loaded state and version. The module is considered
// loaded when its sysfs subdirectory exists. When loaded, modinfo is queried
// for the version; any failure leaves Version empty (unknown).
func (p *VersionProber) Probe(ctx context.Context) ModuleVersion {
	dir := filepath.Join(p.sysModuleDir(), p.module())
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ModuleVersion{Loaded: false}
	}

	out, err := p.runner().Run(ctx, p.modinfo(), "-F", "version", p.module())
	if err != nil {
		return ModuleVersion{Loaded: true}
	}
	return ModuleVersion{Loaded: true, Version: strings.TrimSpace(string(out))}
}
