package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeArchive builds a gzip-compressed tar archive from the given entries,
// keyed by full path (including the top-level directory GitHub prepends).
func makeArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeFetcher returns a fixed archive and records the arguments it received.
type fakeFetcher struct {
	archive          []byte
	err              error
	repo, ref, token string
}

func (f *fakeFetcher) Fetch(_ context.Context, repo, ref, token string) ([]byte, error) {
	f.repo, f.ref, f.token = repo, ref, token
	if f.err != nil {
		return nil, f.err
	}
	return f.archive, nil
}

func TestUpdateRefreshesManifests(t *testing.T) {
	root := t.TempDir()
	// Pre-existing stale release that must be replaced.
	if err := os.MkdirAll(filepath.Join(root, "releases"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "releases", "old.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	archive := makeArchive(t, map[string]string{
		"src-abc123/releases/a.json":     `{"release":"a"}`,
		"src-abc123/releases/b.json":     `{"release":"b"}`,
		"src-abc123/manifests/os.env":    "ID=ubuntu\n",
		"src-abc123/README.md":           "ignored",
		"src-abc123/releases/sub/c.json": "ignored nested",
	})
	f := &fakeFetcher{archive: archive}
	u := &Updater{
		Root:    root,
		Token:   "secret-token",
		Fetcher: f,
		Now:     func() time.Time { return time.Unix(1700000000, 0) },
	}

	res, err := u.Update(context.Background())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if res.ReleaseCount != 2 || res.OSManifestCount != 1 {
		t.Errorf("counts = %d/%d, want 2/1", res.ReleaseCount, res.OSManifestCount)
	}
	if res.Repo != DefaultRepo || res.Ref != DefaultRef {
		t.Errorf("defaults = %s@%s, want %s@%s", res.Repo, res.Ref, DefaultRepo, DefaultRef)
	}
	if f.repo != DefaultRepo || f.ref != DefaultRef || f.token != "secret-token" {
		t.Errorf("fetcher args = %s/%s/%s", f.repo, f.ref, f.token)
	}

	for _, rel := range []string{"a.json", "b.json"} {
		if _, err := os.Stat(filepath.Join(root, "releases", rel)); err != nil {
			t.Errorf("expected %s present: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "old.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale release should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "sub")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("nested entry should not be extracted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "manifests", "os.env")); err != nil {
		t.Errorf("expected os.env present: %v", err)
	}
	marker, err := os.ReadFile(filepath.Join(root, "manifests", "last_update"))
	if err != nil {
		t.Fatalf("expected marker: %v", err)
	}
	if got := string(bytes.TrimSpace(marker)); got != "1700000000" {
		t.Errorf("marker = %q, want 1700000000", got)
	}
}

func TestUpdateUsesConfiguredRepoRef(t *testing.T) {
	archive := makeArchive(t, map[string]string{
		"src/releases/a.json":  `{"release":"a"}`,
		"src/manifests/os.env": "ID=ubuntu\n",
	})
	f := &fakeFetcher{archive: archive}
	u := &Updater{Root: t.TempDir(), Repo: "owner/repo", Ref: "feature/x", Token: "tok", Fetcher: f}

	res, err := u.Update(context.Background())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if res.Repo != "owner/repo" || res.Ref != "feature/x" {
		t.Errorf("source = %s@%s, want owner/repo@feature/x", res.Repo, res.Ref)
	}
	if f.ref != "feature/x" {
		t.Errorf("fetcher ref = %q", f.ref)
	}
}

func TestUpdateRequiresToken(t *testing.T) {
	u := &Updater{Root: t.TempDir(), Fetcher: &fakeFetcher{}}
	if _, err := u.Update(context.Background()); err == nil {
		t.Error("expected error when token is empty")
	}
}

func TestUpdateRejectsInvalidSource(t *testing.T) {
	cases := []struct{ repo, ref string }{
		{"no-slash", "main"},
		{"../..", "main"},
		{"owner/..", "main"},
		{"owner/repo", "../escape"},
		{"owner/repo", "bad ref"},
	}
	for _, c := range cases {
		u := &Updater{Root: t.TempDir(), Repo: c.repo, Ref: c.ref, Token: "tok", Fetcher: &fakeFetcher{}}
		if _, err := u.Update(context.Background()); err == nil {
			t.Errorf("expected error for repo=%q ref=%q", c.repo, c.ref)
		}
	}
}

func TestUpdateRequiresBothDirectories(t *testing.T) {
	noManifests := makeArchive(t, map[string]string{"src/releases/a.json": `{"release":"a"}`})
	noReleases := makeArchive(t, map[string]string{"src/manifests/os.env": "ID=ubuntu\n"})

	for name, archive := range map[string][]byte{"no-manifests": noManifests, "no-releases": noReleases} {
		u := &Updater{Root: t.TempDir(), Token: "tok", Fetcher: &fakeFetcher{archive: archive}}
		if _, err := u.Update(context.Background()); err == nil {
			t.Errorf("%s: expected error for incomplete archive", name)
		}
	}
}

func TestUpdateRejectsTraversalEntries(t *testing.T) {
	root := t.TempDir()
	archive := makeArchive(t, map[string]string{
		"src/releases/a.json":       `{"release":"a"}`,
		"src/manifests/os.env":      "ID=ubuntu\n",
		"src/releases/../evil.json": "pwned",
	})
	u := &Updater{Root: root, Token: "tok", Fetcher: &fakeFetcher{archive: archive}}
	if _, err := u.Update(context.Background()); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("traversal entry must not be written, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "a.json")); err != nil {
		t.Errorf("valid release should still be installed: %v", err)
	}
}

func TestUpdatePropagatesFetchError(t *testing.T) {
	u := &Updater{Root: t.TempDir(), Token: "tok", Fetcher: &fakeFetcher{err: errors.New("boom")}}
	if _, err := u.Update(context.Background()); err == nil {
		t.Error("expected fetch error to propagate")
	}
}

func TestUpdateLockBlocksConcurrentRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".tmp", "update.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	archive := makeArchive(t, map[string]string{
		"src/releases/a.json":  `{"release":"a"}`,
		"src/manifests/os.env": "ID=ubuntu\n",
	})
	u := &Updater{Root: root, Token: "tok", Fetcher: &fakeFetcher{archive: archive}}
	if _, err := u.Update(context.Background()); err == nil {
		t.Error("expected error while an update lock is held")
	}
}

func TestExtractRejectsDuplicateEntries(t *testing.T) {
	// Two distinct tar entries that normalize to the same release file.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"src/releases/a.json", "./src/releases/a.json"} {
		body := []byte(`{"release":"a"}`)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	if _, err := extractManifests(buf.Bytes()); err == nil {
		t.Error("expected error for duplicate manifest entries")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSwapRestoresOnManifestInstallFailure(t *testing.T) {
	root := t.TempDir()
	u := &Updater{Root: root}
	mustWrite(t, filepath.Join(root, "releases", "old.json"), "OLD")
	mustWrite(t, filepath.Join(root, "manifests", "old.env"), "OLD")

	// Stage under root so renames stay on the same filesystem.
	work, err := os.MkdirTemp(root, "work-")
	if err != nil {
		t.Fatal(err)
	}
	stagingReleases := filepath.Join(work, "releases")
	mustWrite(t, filepath.Join(stagingReleases, "new.json"), "NEW")
	stagingManifests := filepath.Join(work, "manifests") // intentionally absent
	backup := filepath.Join(work, "backup")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := u.swap(stagingReleases, stagingManifests, backup); err == nil {
		t.Fatal("expected swap to fail when staging manifests is missing")
	}

	if b, _ := os.ReadFile(filepath.Join(root, "releases", "old.json")); string(b) != "OLD" {
		t.Errorf("releases not restored: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "manifests", "old.env")); string(b) != "OLD" {
		t.Errorf("manifests not restored: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "new.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partially installed release should be rolled back, stat err = %v", err)
	}
}

func TestClassifyEntry(t *testing.T) {
	accept := map[string][2]string{
		"top/releases/a.json":     {"releases", "a.json"},
		"./top/manifests/b.env":   {"manifests", "b.env"},
		"o-r-sha/releases/x.json": {"releases", "x.json"},
	}
	for in, want := range accept {
		c, b, ok := classifyEntry(in)
		if !ok || c != want[0] || b != want[1] {
			t.Errorf("classifyEntry(%q) = %q,%q,%v; want %q,%q,true", in, c, b, ok, want[0], want[1])
		}
	}

	reject := []string{
		"/abs/releases/a.json",
		"top/releases/../a.json",
		"top/releases/sub/a.json",
		"releases/a.json",
		"top/releases/a.txt",
		"top/other/a.json",
		"top\\releases\\a.json",
		"top/releases/.",
		"top/manifests/a.json",
	}
	for _, in := range reject {
		if _, _, ok := classifyEntry(in); ok {
			t.Errorf("classifyEntry(%q) should be rejected", in)
		}
	}
}
