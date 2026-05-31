package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tetsuh/tt-env-go/pkg/manifest"
	packagemanager "github.com/tetsuh/tt-env-go/pkg/package_manager"
)

// validateDownloadComponents checks that the component set is non-empty and that
// each component has a safe name and the download metadata required by the
// download path. It performs no I/O so it can be reused by the dry-run.
func validateDownloadComponents(components map[string]manifest.Component) error {
	if len(components) == 0 {
		return fmt.Errorf("install: USE_SYSTEM_PACKAGES=false but the stack manifest declares no downloadable components")
	}
	for name, c := range components {
		if err := validateComponentName(name); err != nil {
			return err
		}
		if c.DownloadURL == "" || c.SHA256 == "" {
			return fmt.Errorf("install: component %q must declare both download_url and sha256 when USE_SYSTEM_PACKAGES=false", name)
		}
		if err := validateDownloadURL(c.DownloadURL); err != nil {
			return fmt.Errorf("install: component %q: %w", name, err)
		}
	}
	return nil
}

// downloadComponents downloads each component declared in components into
// stagingDir/artifacts and verifies its SHA-256 checksum. It mirrors proto1's
// _install_download_components, which is used when the OS manifest sets
// USE_SYSTEM_PACKAGES=false. Each component must declare both a download URL and
// a sha256 checksum.
func (o *Orchestrator) downloadComponents(ctx context.Context, stagingDir string, components map[string]manifest.Component) error {
	if err := validateDownloadComponents(components); err != nil {
		return err
	}

	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)

	artifactsDir := filepath.Join(stagingDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return fmt.Errorf("install: create artifacts directory: %w", err)
	}

	runner := o.runner()
	for _, name := range names {
		c := components[name]
		artifactPath := filepath.Join(artifactsDir, name)
		o.logf("Downloading component %s", name)
		if _, err := runner.Run(ctx, "curl", "--fail", "--location", "--retry", "3", "--output", artifactPath, "--", c.DownloadURL); err != nil {
			return fmt.Errorf("install: download component %q: %w", name, err)
		}

		sum, err := sha256File(artifactPath)
		if err != nil {
			return fmt.Errorf("install: checksum component %q: %w", name, err)
		}
		if !strings.EqualFold(sum, c.SHA256) {
			return fmt.Errorf("install: component %q checksum mismatch: got %s, want %s", name, sum, c.SHA256)
		}
		o.logf("Verified component %s checksum", name)
	}
	return nil
}

// runner returns the configured command runner, defaulting to an exec runner.
func (o *Orchestrator) runner() packagemanager.CommandRunner {
	if o.Runner != nil {
		return o.Runner
	}
	return packagemanager.ExecRunner{}
}

// validateDownloadURL ensures rawURL is an absolute http or https URL.
func validateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid download URL %q: %w", rawURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("download URL %q must be an absolute http(s) URL", rawURL)
	}
	return nil
}

// sha256File returns the lowercase hex SHA-256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
