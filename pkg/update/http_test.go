package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPFetcherFetchesArchive(t *testing.T) {
	var gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("archive-bytes"))
	}))
	defer srv.Close()

	f := HTTPFetcher{BaseURL: srv.URL}
	data, err := f.Fetch(context.Background(), "owner/repo", "feature/x", "tok")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(data) != "archive-bytes" {
		t.Errorf("data = %q", data)
	}
	if gotPath != "/repos/owner/repo/tarball/feature/x" {
		t.Errorf("path = %q, want /repos/owner/repo/tarball/feature/x", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("accept = %q", gotAccept)
	}
}

func TestHTTPFetcherAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	f := HTTPFetcher{BaseURL: srv.URL}
	if _, err := f.Fetch(context.Background(), "owner/repo", "main", "tok"); err == nil {
		t.Error("expected error for HTTP 401")
	}
}

func TestHTTPFetcherNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := HTTPFetcher{BaseURL: srv.URL}
	if _, err := f.Fetch(context.Background(), "owner/repo", "main", "tok"); err == nil {
		t.Error("expected error for HTTP 404")
	}
}
