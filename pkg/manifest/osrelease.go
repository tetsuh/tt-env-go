package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// OSInfo holds the OS identity parsed from an os-release file. VersionCodename
// and UbuntuCodename are used to resolve derivative distros to a parent distro
// manifest.
type OSInfo struct {
	ID              string
	VersionID       string
	IDLike          []string
	VersionCodename string
	UbuntuCodename  string
}

// codenameToUbuntuVersion maps the supported Ubuntu release codenames to their
// version, used to resolve a derivative distro (e.g. Linux Mint) to its parent
// Ubuntu manifest.
var codenameToUbuntuVersion = map[string]string{
	"noble": "24.04",
	"jammy": "22.04",
}

// DetectOS resolves the running OS. If TT_OVERRIDE_OS_ID and
// TT_OVERRIDE_OS_VERSION are both set it returns the overridden identity without
// reading the file; otherwise it parses the os-release file at path.
func DetectOS(path string) (*OSInfo, error) {
	overrideID := os.Getenv("TT_OVERRIDE_OS_ID")
	overrideVersion := os.Getenv("TT_OVERRIDE_OS_VERSION")
	if overrideID != "" || overrideVersion != "" {
		if overrideID == "" || overrideVersion == "" {
			return nil, fmt.Errorf("both TT_OVERRIDE_OS_ID and TT_OVERRIDE_OS_VERSION must be set for OS override")
		}
		return &OSInfo{ID: overrideID, VersionID: overrideVersion}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("detect os: %w", err)
	}
	return ParseOSRelease(data)
}

// ParseOSRelease parses os-release file content into an OSInfo. It returns an
// error if the required ID or VERSION_ID fields are absent.
func ParseOSRelease(data []byte) (*OSInfo, error) {
	info := &OSInfo{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = unquoteOSReleaseValue(value)

		switch key {
		case "ID":
			info.ID = value
		case "VERSION_ID":
			info.VersionID = value
		case "ID_LIKE":
			info.IDLike = strings.Fields(value)
		case "VERSION_CODENAME":
			info.VersionCodename = value
		case "UBUNTU_CODENAME":
			info.UbuntuCodename = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse os-release: %w", err)
	}

	if info.ID == "" || info.VersionID == "" {
		return nil, fmt.Errorf("os-release is missing required ID or VERSION_ID")
	}
	return info, nil
}

// unquoteOSReleaseValue strips an optional surrounding pair of single or double
// quotes and a trailing carriage return from an os-release value.
func unquoteOSReleaseValue(value string) string {
	value = strings.TrimSuffix(value, "\r")
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return value
}

// ManifestCandidates returns the ordered manifest keys to try for this OS, most
// specific first: the exact <id>-<version>, then the parent Ubuntu manifest
// resolved from the release codename. Keys are suffixless (the manifest filename
// is <key>.env) and de-duplicated.
func (o *OSInfo) ManifestCandidates() []string {
	var candidates []string
	seen := make(map[string]bool)
	add := func(key string) {
		if key == "-" || seen[key] {
			return
		}
		seen[key] = true
		candidates = append(candidates, key)
	}

	add(o.ID + "-" + o.VersionID)

	if o.ID == "ubuntu" || o.isLike("ubuntu") {
		codename := o.UbuntuCodename
		if codename == "" {
			codename = o.VersionCodename
		}
		if version, ok := codenameToUbuntuVersion[codename]; ok {
			add("ubuntu-" + version)
		}
	}

	return candidates
}

// ResolveManifestKey returns the first manifest key from ManifestCandidates that
// is present in available. Keys are suffixless (e.g. "ubuntu-24.04"); available
// must use the same suffixless form. It returns an error if none match.
func (o *OSInfo) ResolveManifestKey(available []string) (string, error) {
	set := make(map[string]bool, len(available))
	for _, key := range available {
		set[key] = true
	}

	candidates := o.ManifestCandidates()
	for _, candidate := range candidates {
		if set[candidate] {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no manifest found for %s %s (tried: %s)", o.ID, o.VersionID, strings.Join(candidates, ", "))
}

// isLike reports whether id appears in the OS ID_LIKE list.
func (o *OSInfo) isLike(id string) bool {
	for _, like := range o.IDLike {
		if like == id {
			return true
		}
	}
	return false
}
