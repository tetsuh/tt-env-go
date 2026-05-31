package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Size limits guarding against malformed or hostile archives (decompression
// bombs). Repo and ref are overridable, so these caps apply to all sources.
const (
	maxFileBytes         = 16 << 20  // per manifest file
	maxUncompressedBytes = 256 << 20 // total extracted bytes
)

// manifestSet holds the release and OS manifest files extracted from an
// archive, keyed by their base file name.
type manifestSet struct {
	releases    map[string][]byte
	osManifests map[string][]byte
}

// extractManifests reads a gzip-compressed tar archive and returns the
// release (.json) and OS manifest (.env) files found directly under the
// archive's top-level releases/ and manifests/ directories. It mirrors proto1's
// "tar --strip-components=1" handling and rejects unsafe or oversized entries.
func extractManifests(archive []byte) (*manifestSet, error) {
	if len(archive) == 0 {
		return nil, errors.New("update: empty manifest archive")
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("update: open manifest archive: %w", err)
	}
	defer gz.Close()

	set := &manifestSet{
		releases:    map[string][]byte{},
		osManifests: map[string][]byte{},
	}
	tr := tar.NewReader(gz)
	var total int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("update: read manifest archive: %w", err)
		}
		if !hdr.FileInfo().Mode().IsRegular() {
			continue
		}
		// Account for every regular file (including ignored ones) before
		// classification so a hostile archive cannot bypass the decompression
		// cap by hiding large bodies outside releases/ and manifests/.
		if hdr.Size < 0 {
			return nil, fmt.Errorf("update: manifest entry %q has an invalid size", hdr.Name)
		}
		total += hdr.Size
		if total > maxUncompressedBytes {
			return nil, errors.New("update: manifest archive exceeds the total size limit")
		}

		category, name, ok := classifyEntry(hdr.Name)
		if !ok {
			continue
		}
		if hdr.Size > maxFileBytes {
			return nil, fmt.Errorf("update: manifest entry %q exceeds the size limit", hdr.Name)
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxFileBytes+1))
		if err != nil {
			return nil, fmt.Errorf("update: read manifest entry %q: %w", hdr.Name, err)
		}
		if int64(len(data)) > maxFileBytes {
			return nil, fmt.Errorf("update: manifest entry %q exceeds the size limit", hdr.Name)
		}

		target := set.releases
		if category == "manifests" {
			target = set.osManifests
		}
		if _, dup := target[name]; dup {
			return nil, fmt.Errorf("update: duplicate manifest entry %s/%s", category, name)
		}
		target[name] = data
	}

	if len(set.releases) == 0 {
		return nil, errors.New("update: manifest archive contains no release manifests")
	}
	if len(set.osManifests) == 0 {
		return nil, errors.New("update: manifest archive contains no OS manifests")
	}
	return set, nil
}

// classifyEntry interprets a tar entry name after stripping the archive's
// top-level directory (proto1 --strip-components=1). It returns the category
// ("releases" or "manifests") and base file name for entries of the form
// "<top>/releases/<name>.json" or "<top>/manifests/<name>.env", rejecting any
// path containing backslashes, empty/"."/".." segments, or extra nesting.
func classifyEntry(name string) (category, base string, ok bool) {
	if strings.ContainsRune(name, '\\') {
		return "", "", false
	}
	name = strings.TrimPrefix(name, "./")

	parts := strings.Split(name, "/")
	if len(parts) != 3 {
		return "", "", false
	}
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return "", "", false
		}
	}

	category = parts[1]
	base = parts[2]
	switch category {
	case "releases":
		if !strings.HasSuffix(base, ".json") {
			return "", "", false
		}
	case "manifests":
		if !strings.HasSuffix(base, ".env") {
			return "", "", false
		}
	default:
		return "", "", false
	}
	return category, base, true
}
