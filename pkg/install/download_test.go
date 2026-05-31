package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// curlRunner returns a MockRunner whose RunFunc writes payloads keyed by URL to
// the curl --output path, emulating a successful download.
func curlRunner(payloads map[string][]byte) *packagemanager.MockRunner {
	r := &packagemanager.MockRunner{}
	r.RunFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "curl" {
			return nil, nil
		}
		var out, url string
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				out = args[i+1]
			}
		}
		url = args[len(args)-1]
		if body, ok := payloads[url]; ok && out != "" {
			_ = os.WriteFile(out, body, 0o644)
		}
		return nil, nil
	}
	return r
}

func TestDownloadComponentsVerifiesChecksum(t *testing.T) {
	body := []byte("artifact-bytes")
	url := "https://example.test/kmd.tar.gz"
	runner := curlRunner(map[string][]byte{url: body})
	orch := &Orchestrator{Root: t.TempDir(), Runner: runner, Logf: func(string, ...any) {}}

	staging := t.TempDir()
	comps := map[string]manifest.Component{
		"tt-kmd": {Version: "1", DownloadURL: url, SHA256: sha256Hex(body)},
	}
	if err := orch.downloadComponents(context.Background(), staging, comps); err != nil {
		t.Fatalf("downloadComponents: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "artifacts", "tt-kmd")); err != nil {
		t.Errorf("expected downloaded artifact: %v", err)
	}
}

func TestDownloadComponentsChecksumMismatch(t *testing.T) {
	url := "https://example.test/a"
	runner := curlRunner(map[string][]byte{url: []byte("real")})
	orch := &Orchestrator{Root: t.TempDir(), Runner: runner, Logf: func(string, ...any) {}}

	comps := map[string]manifest.Component{
		"tt-kmd": {Version: "1", DownloadURL: url, SHA256: sha256Hex([]byte("expected-different"))},
	}
	err := orch.downloadComponents(context.Background(), t.TempDir(), comps)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
}

func TestDownloadComponentsRequiresFields(t *testing.T) {
	orch := &Orchestrator{Root: t.TempDir(), Runner: &packagemanager.MockRunner{}, Logf: func(string, ...any) {}}
	cases := map[string]manifest.Component{
		"missing-sha": {Version: "1", DownloadURL: "https://example.test/a"},
		"missing-url": {Version: "1", SHA256: sha256Hex([]byte("x"))},
	}
	for name, comp := range cases {
		t.Run(name, func(t *testing.T) {
			err := orch.downloadComponents(context.Background(), t.TempDir(),
				map[string]manifest.Component{"c": comp})
			if err == nil {
				t.Fatal("expected error for incomplete component")
			}
		})
	}
}

func TestDownloadComponentsRejectsNonHTTPURL(t *testing.T) {
	orch := &Orchestrator{Root: t.TempDir(), Runner: &packagemanager.MockRunner{}, Logf: func(string, ...any) {}}
	comps := map[string]manifest.Component{
		"c": {Version: "1", DownloadURL: "file:///etc/passwd", SHA256: sha256Hex([]byte("x"))},
	}
	if err := orch.downloadComponents(context.Background(), t.TempDir(), comps); err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestDownloadComponentsEmptyIsError(t *testing.T) {
	orch := &Orchestrator{Root: t.TempDir(), Logf: func(string, ...any) {}}
	if err := orch.downloadComponents(context.Background(), t.TempDir(), nil); err == nil {
		t.Fatal("expected error when no components are declared")
	}
}
