package update

import (
	"context"
	"errors"
	"strings"
	"testing"

	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

func TestCurlFetcherReturnsArchive(t *testing.T) {
	mock := &packagemanager.MockRunner{
		Responses: []packagemanager.CommandResponse{{Output: []byte("archive-bytes")}},
	}
	f := CurlFetcher{Runner: mock, BaseURL: "https://example.invalid"}

	data, err := f.Fetch(context.Background(), "owner/repo", "feature/x", "secret-token")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(data) != "archive-bytes" {
		t.Errorf("data = %q", data)
	}

	if len(mock.Commands) != 1 || mock.Commands[0].Name != "curl" {
		t.Fatalf("expected one curl invocation, got %v", mock.CommandStrings())
	}
	joined := mock.Commands[0].String()
	if !strings.Contains(joined, "https://example.invalid/repos/owner/repo/tarball/feature/x") {
		t.Errorf("tarball URL missing or wrong: %s", joined)
	}
	if strings.Contains(joined, "secret-token") {
		t.Errorf("token must not appear in argv: %s", joined)
	}
	if !strings.Contains(joined, "--max-filesize") {
		t.Errorf("expected --max-filesize size cap in argv: %s", joined)
	}
}

func TestCurlFetcherPropagatesError(t *testing.T) {
	mock := &packagemanager.MockRunner{
		Responses: []packagemanager.CommandResponse{{
			Output: []byte("curl: (22) The requested URL returned error: 404"),
			Err:    errors.New("exit status 22"),
		}},
	}
	f := CurlFetcher{Runner: mock}
	if _, err := f.Fetch(context.Background(), "owner/repo", "main", "tok"); err == nil {
		t.Error("expected error when curl fails")
	}
}

func TestCurlFetcherRejectsEmptyArchive(t *testing.T) {
	mock := &packagemanager.MockRunner{
		Responses: []packagemanager.CommandResponse{{Output: nil}},
	}
	f := CurlFetcher{Runner: mock}
	if _, err := f.Fetch(context.Background(), "owner/repo", "main", "tok"); err == nil {
		t.Error("expected error for an empty archive")
	}
}
