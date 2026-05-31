package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// maxCompressedBytes caps the downloaded archive size to guard against hostile
// or runaway responses.
const maxCompressedBytes = 64 << 20

// DefaultCurl is the curl executable used when CurlFetcher does not override it.
const DefaultCurl = "curl"

// CurlFetcher downloads the manifest archive from the GitHub tarball API using
// curl, mirroring proto1 which also shells out to curl. Running through the
// CommandRunner abstraction keeps the fetch unit-testable and avoids linking
// Go's TLS stack into the binary.
type CurlFetcher struct {
	// Runner executes curl; ExecRunner is used when nil.
	Runner packagemanager.CommandRunner
	// Curl is the curl executable to invoke; DefaultCurl is used when empty.
	Curl string
	// BaseURL overrides the GitHub API base, primarily for tests; it defaults to
	// https://api.github.com.
	BaseURL string
}

func (f CurlFetcher) runner() packagemanager.CommandRunner {
	if f.Runner != nil {
		return f.Runner
	}
	return packagemanager.ExecRunner{}
}

func (f CurlFetcher) curl() string {
	if f.Curl != "" {
		return f.Curl
	}
	return DefaultCurl
}

// Fetch downloads the gzip-compressed tar archive for repo at ref. The ref is
// placed directly after /tarball/ without path cleaning so refs containing "/"
// are preserved; the caller validates repo and ref beforehand. The auth token
// is passed through a 0600 header file rather than argv so it never appears in
// the process list or any command logging.
func (f CurlFetcher) Fetch(ctx context.Context, repo, ref, token string) ([]byte, error) {
	base := f.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/tarball/%s", base, repo, ref)

	headerFile, cleanup, err := writeAuthHeaders(token)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	args := []string{
		"--location",
		"--retry", "3",
		"--silent",
		"--show-error",
		"--fail",
		"--header", "@" + headerFile,
		"--output", "-",
		url,
	}
	out, err := f.runner().Run(ctx, f.curl(), args...)
	if err != nil {
		// out may carry curl's stderr, but never the token (it is in the header
		// file, not argv).
		if msg := bytes.TrimSpace(out); len(msg) > 0 {
			return nil, fmt.Errorf("update: fetch manifests: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("update: fetch manifests: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("update: empty manifest archive")
	}
	if int64(len(out)) > maxCompressedBytes {
		return nil, errors.New("update: manifest archive exceeds the download size limit")
	}
	return out, nil
}

// writeAuthHeaders writes the GitHub auth headers to a private temporary file
// and returns its path together with a cleanup function.
func writeAuthHeaders(token string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "tt-env-update-")
	if err != nil {
		return "", nil, fmt.Errorf("update: create header directory: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	path := filepath.Join(dir, "headers")
	content := "Authorization: Bearer " + token + "\nAccept: application/vnd.github+json\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("update: write authentication headers: %w", err)
	}
	return path, cleanup, nil
}
