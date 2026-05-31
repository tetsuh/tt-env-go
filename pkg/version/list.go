package version

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// List returns the names of all fully-installed releases under VersionsDir,
// sorted ascending. Releases whose directory exists but lacks a valid installed
// marker, and internal staging directories (".partial"/".backup"), are skipped.
// It returns an empty result, without error, when nothing is installed or the
// versions directory does not exist.
func (i *Installer) List() ([]string, error) {
	entries, err := os.ReadDir(i.VersionsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("version: read versions dir: %w", err)
	}

	var releases []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if i.IsInstalled(name) {
			releases = append(releases, name)
		}
	}
	sort.Strings(releases)
	return releases, nil
}
