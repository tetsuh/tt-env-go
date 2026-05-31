package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// OSManifest holds the values parsed from an OS .env manifest. Scalars holds
// KEY="value" assignments; Lists holds KEY=(...) array assignments. The grammar
// is intentionally restrictive and mirrors the prototype's parser: it never
// sources the file and rejects lines containing shell metacharacters.
type OSManifest struct {
	Scalars map[string]string
	Lists   map[string][]string
}

var (
	osManifestScalarRe      = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)="([A-Za-z0-9_./:+-]*)"\s*$`)
	osManifestEmptyArrayRe  = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)=\(\s*\)\s*$`)
	osManifestInlineArrayRe = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)=\(\s*(.*\S)\s*\)\s*$`)
	osManifestOpenArrayRe   = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)=\(\s*$`)
	osManifestCloseArrayRe  = regexp.MustCompile(`^\s*\)\s*$`)
	osManifestBlankRe       = regexp.MustCompile(`^\s*(#.*)?$`)
	osManifestQuotedTokenRe = regexp.MustCompile(`^"([A-Za-z0-9_./:+-]*)"(.*)$`)
	osManifestBareTokenRe   = regexp.MustCompile(`^([A-Za-z0-9_./:+-]+)(.*)$`)
	osManifestKeyRe         = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// osManifestVirtualReplacer normalizes virtual package names: '-', '.' and '+'
// become '_' before the VIRT_PKG_ prefix is applied.
var osManifestVirtualReplacer = strings.NewReplacer("-", "_", ".", "_", "+", "_")

// LoadOSManifest reads and parses the OS .env manifest at path.
func LoadOSManifest(path string) (*OSManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load os manifest: %w", err)
	}
	return ParseOSManifest(data)
}

// ParseOSManifest parses OS .env manifest content. It accepts blank and comment
// lines, KEY="value" scalars, and KEY=(...) arrays (empty, inline, or spanning
// multiple lines). Lines containing shell metacharacters or otherwise not
// matching the grammar are rejected with an error.
func ParseOSManifest(data []byte) (*OSManifest, error) {
	m := &OSManifest{
		Scalars: make(map[string]string),
		Lists:   make(map[string][]string),
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	openKey := ""
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")

		if osManifestBlankRe.MatchString(line) {
			continue
		}
		if hasDangerousChars(line) {
			return nil, fmt.Errorf("os manifest line %d: rejected unsafe characters: %s", lineNo, line)
		}

		if openKey != "" {
			if osManifestCloseArrayRe.MatchString(line) {
				openKey = ""
				continue
			}
			values, err := parseListTokens(strings.TrimSpace(line))
			if err != nil {
				return nil, fmt.Errorf("os manifest line %d: %w", lineNo, err)
			}
			m.Lists[openKey] = append(m.Lists[openKey], values...)
			continue
		}

		switch {
		case osManifestScalarRe.MatchString(line):
			groups := osManifestScalarRe.FindStringSubmatch(line)
			m.Scalars[groups[1]] = groups[2]
		case osManifestEmptyArrayRe.MatchString(line):
			groups := osManifestEmptyArrayRe.FindStringSubmatch(line)
			m.Lists[groups[1]] = []string{}
		case osManifestInlineArrayRe.MatchString(line):
			groups := osManifestInlineArrayRe.FindStringSubmatch(line)
			values, err := parseListTokens(groups[2])
			if err != nil {
				return nil, fmt.Errorf("os manifest line %d: %w", lineNo, err)
			}
			m.Lists[groups[1]] = values
		case osManifestOpenArrayRe.MatchString(line):
			groups := osManifestOpenArrayRe.FindStringSubmatch(line)
			openKey = groups[1]
			if _, ok := m.Lists[openKey]; !ok {
				m.Lists[openKey] = []string{}
			}
		default:
			return nil, fmt.Errorf("os manifest line %d: invalid syntax: %s", lineNo, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse os manifest: %w", err)
	}
	if openKey != "" {
		return nil, fmt.Errorf("os manifest: unterminated array: %s", openKey)
	}
	return m, nil
}

// hasDangerousChars reports whether a manifest line contains shell
// metacharacters that the restrictive grammar forbids.
func hasDangerousChars(line string) bool {
	return strings.ContainsAny(line, "$`;\\")
}

// parseListTokens splits a manifest array body into its quoted or bare tokens,
// stopping at an inline comment.
func parseListTokens(text string) ([]string, error) {
	var values []string
	for {
		text = strings.TrimLeft(text, " \t")
		if text == "" || strings.HasPrefix(text, "#") {
			return values, nil
		}
		if groups := osManifestQuotedTokenRe.FindStringSubmatch(text); groups != nil {
			values = append(values, groups[1])
			text = groups[2]
			continue
		}
		if groups := osManifestBareTokenRe.FindStringSubmatch(text); groups != nil {
			values = append(values, groups[1])
			text = groups[2]
			continue
		}
		return nil, fmt.Errorf("invalid array item: %s", text)
	}
}

// PackageManager returns the native package manager named by the manifest's
// PKG_MANAGER scalar (e.g. "apt" or "dnf").
func (m *OSManifest) PackageManager() string {
	return m.Scalars["PKG_MANAGER"]
}

// UseSystemPackages reports whether the manifest opts into installing packages
// with the native package manager (USE_SYSTEM_PACKAGES="true").
func (m *OSManifest) UseSystemPackages() bool {
	return m.Scalars["USE_SYSTEM_PACKAGES"] == "true"
}

// RequiredRepos returns the repositories the manifest requires before installing
// system packages (REQUIRED_REPOS array).
func (m *OSManifest) RequiredRepos() []string {
	return m.Lists["REQUIRED_REPOS"]
}

// ResolvePackage maps a virtual package name (e.g. "cmake", "tt-smi") to its
// concrete package name via the manifest's VIRT_PKG_* scalars. The lookup is
// case-insensitive and treats '-', '.' and '+' as '_'. It returns false if the
// virtual package is not defined.
func (m *OSManifest) ResolvePackage(virtual string) (string, bool) {
	key := "VIRT_PKG_" + osManifestVirtualReplacer.Replace(strings.ToUpper(virtual))
	if !osManifestKeyRe.MatchString(key) {
		return "", false
	}
	value, ok := m.Scalars[key]
	if !ok || value == "" {
		return "", false
	}
	return value, true
}
