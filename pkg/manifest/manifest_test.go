package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidFixture(t *testing.T) {
	m, err := Load(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}

	if m.Release != "2026.05.16" {
		t.Errorf("Release = %q, want %q", m.Release, "2026.05.16")
	}
	if m.Description != "Tenstorrent proto sample stack 2026.05.16" {
		t.Errorf("Description = %q, want sample description", m.Description)
	}

	wantLens := map[string]int{
		"components":           len(m.Components),
		"system_packages":      len(m.SystemPackages),
		"python_packages":      len(m.PythonPackages),
		"git_components":       len(m.GitComponents),
		"container_components": len(m.ContainerComponents),
	}
	expected := map[string]int{
		"components":           4,
		"system_packages":      5,
		"python_packages":      5,
		"git_components":       2,
		"container_components": 4,
	}
	for section, want := range expected {
		if got := wantLens[section]; got != want {
			t.Errorf("%s: got %d entries, want %d", section, got, want)
		}
	}

	if got := m.Components["tt-kmd"].Version; got != "ttkmd-2.8.0" {
		t.Errorf("components[tt-kmd] = %q, want %q", got, "ttkmd-2.8.0")
	}
	if got := m.SystemPackages["metalium"]; got != "0.69.0~ubuntu24.04" {
		t.Errorf("system_packages[metalium] = %q, want %q", got, "0.69.0~ubuntu24.04")
	}

	gc, ok := m.GitComponents["tt-studio"]
	if !ok {
		t.Fatal("git_components missing tt-studio")
	}
	if gc.URL != "https://github.com/tenstorrent/tt-studio.git" {
		t.Errorf("tt-studio url = %q", gc.URL)
	}
	if gc.Version != "a6d347af3980540bb16d10ec473a6b09ce6f2138" {
		t.Errorf("tt-studio version = %q", gc.Version)
	}

	ref, ok := m.ContainerComponents["tt-metalium"]
	if !ok {
		t.Fatal("container_components missing tt-metalium")
	}
	if ref.Ref != "tt-metalium-ubuntu24" {
		t.Errorf("tt-metalium ref = %q, want %q", ref.Ref, "tt-metalium-ubuntu24")
	}

	img, ok := m.ContainerComponents["tt-metalium-ubuntu24"]
	if !ok {
		t.Fatal("container_components missing tt-metalium-ubuntu24")
	}
	if img.ImageURL == "" || img.ImageTag == "" {
		t.Errorf("tt-metalium-ubuntu24 image fields not populated: %+v", img)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join("testdata", "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	path := writeTempManifest(t, "{ this is not valid json ")
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		m       Manifest
		wantErr bool
	}{
		{
			name:    "valid minimal",
			m:       Manifest{Release: "2026.05.16"},
			wantErr: false,
		},
		{
			name:    "missing release",
			m:       Manifest{Description: "no release id"},
			wantErr: true,
		},
		{
			name: "git component missing url",
			m: Manifest{
				Release:       "r",
				GitComponents: map[string]GitComponent{"x": {Version: "v1"}},
			},
			wantErr: true,
		},
		{
			name: "git component missing version",
			m: Manifest{
				Release:       "r",
				GitComponents: map[string]GitComponent{"x": {URL: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name: "container component ref only",
			m: Manifest{
				Release:             "r",
				ContainerComponents: map[string]ContainerComponent{"x": {Ref: "y"}},
			},
			wantErr: false,
		},
		{
			name: "container component image pair",
			m: Manifest{
				Release:             "r",
				ContainerComponents: map[string]ContainerComponent{"x": {ImageURL: "ghcr.io/x", ImageTag: "sha256:abc"}},
			},
			wantErr: false,
		},
		{
			name: "container component empty",
			m: Manifest{
				Release:             "r",
				ContainerComponents: map[string]ContainerComponent{"x": {}},
			},
			wantErr: true,
		},
		{
			name: "container component partial image",
			m: Manifest{
				Release:             "r",
				ContainerComponents: map[string]ContainerComponent{"x": {ImageURL: "ghcr.io/x"}},
			},
			wantErr: true,
		},
		{
			name: "container component ref and image",
			m: Manifest{
				Release:             "r",
				ContainerComponents: map[string]ContainerComponent{"x": {Ref: "y", ImageURL: "ghcr.io/x", ImageTag: "sha256:abc"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func writeTempManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp manifest: %v", err)
	}
	return path
}
