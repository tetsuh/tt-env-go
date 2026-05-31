package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxCompressedBytes caps the downloaded archive size to guard against hostile
// or runaway responses.
const maxCompressedBytes = 64 << 20

// HTTPFetcher downloads the manifest archive from the GitHub tarball API.
type HTTPFetcher struct {
	// Client issues the request; http.DefaultClient is used when nil.
	Client *http.Client
	// BaseURL overrides the GitHub API base, primarily for tests; it defaults to
	// https://api.github.com.
	BaseURL string
}

// Fetch downloads the gzip-compressed tar archive for repo at ref. The ref is
// placed directly after /tarball/ without path cleaning so refs containing "/"
// are preserved; the caller validates repo and ref beforehand.
func (f HTTPFetcher) Fetch(ctx context.Context, repo, ref, token string) ([]byte, error) {
	base := f.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/tarball/%s", base, repo, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: build manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: fetch manifests: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("update: authentication failed fetching manifests (HTTP %d); check GITHUB_TOKEN or run: gh auth login", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("update: failed to fetch manifests (HTTP %d)", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCompressedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("update: read manifest archive: %w", err)
	}
	if int64(len(data)) > maxCompressedBytes {
		return nil, errors.New("update: manifest archive exceeds the download size limit")
	}
	return data, nil
}
