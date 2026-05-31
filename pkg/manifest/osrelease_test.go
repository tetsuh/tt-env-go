package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

func TestParseOSReleaseUbuntu(t *testing.T) {
	info, err := ParseOSRelease(readFixture(t, "os-release-ubuntu.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "ubuntu" {
		t.Errorf("ID = %q, want ubuntu", info.ID)
	}
	if info.VersionID != "24.04" {
		t.Errorf("VersionID = %q, want 24.04", info.VersionID)
	}
	if info.UbuntuCodename != "noble" {
		t.Errorf("UbuntuCodename = %q, want noble", info.UbuntuCodename)
	}
}

func TestParseOSReleaseLinuxMint(t *testing.T) {
	info, err := ParseOSRelease(readFixture(t, "os-release-linuxmint.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "linuxmint" {
		t.Errorf("ID = %q, want linuxmint", info.ID)
	}
	if info.VersionID != "22.1" {
		t.Errorf("VersionID = %q, want 22.1", info.VersionID)
	}
	wantLike := []string{"ubuntu", "debian"}
	if len(info.IDLike) != len(wantLike) {
		t.Fatalf("IDLike = %v, want %v", info.IDLike, wantLike)
	}
	for i, v := range wantLike {
		if info.IDLike[i] != v {
			t.Errorf("IDLike[%d] = %q, want %q", i, info.IDLike[i], v)
		}
	}
	if info.UbuntuCodename != "noble" {
		t.Errorf("UbuntuCodename = %q, want noble", info.UbuntuCodename)
	}
}

func TestParseOSReleaseFedora(t *testing.T) {
	info, err := ParseOSRelease(readFixture(t, "os-release-fedora.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "fedora" {
		t.Errorf("ID = %q, want fedora", info.ID)
	}
	if info.VersionID != "40" {
		t.Errorf("VersionID = %q, want 40", info.VersionID)
	}
	if len(info.IDLike) != 0 {
		t.Errorf("IDLike = %v, want empty", info.IDLike)
	}
}

func TestParseOSReleaseMissingFields(t *testing.T) {
	cases := map[string]string{
		"missing both":       "NAME=\"X\"\n",
		"missing version_id": "ID=ubuntu\n",
		"missing id":         "VERSION_ID=\"24.04\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseOSRelease([]byte(content)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDetectOSFromFile(t *testing.T) {
	info, err := DetectOS(filepath.Join("testdata", "os-release-ubuntu.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "ubuntu" || info.VersionID != "24.04" {
		t.Errorf("got %s %s, want ubuntu 24.04", info.ID, info.VersionID)
	}
}

func TestDetectOSMissingFile(t *testing.T) {
	if _, err := DetectOS(filepath.Join("testdata", "no-such-os-release")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestDetectOSEmptyPathDefaults(t *testing.T) {
	if _, err := os.Stat("/etc/os-release"); err != nil {
		t.Skip("/etc/os-release not present")
	}
	// An empty path must fall back to /etc/os-release rather than failing to
	// open the empty path.
	if _, err := DetectOS(""); err != nil {
		t.Fatalf("DetectOS(\"\") should default to /etc/os-release: %v", err)
	}
}

func TestDetectOSOverride(t *testing.T) {
	t.Setenv("TT_OVERRIDE_OS_ID", "ubuntu")
	t.Setenv("TT_OVERRIDE_OS_VERSION", "22.04")
	info, err := DetectOS(filepath.Join("testdata", "no-such-os-release"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "ubuntu" || info.VersionID != "22.04" {
		t.Errorf("got %s %s, want ubuntu 22.04", info.ID, info.VersionID)
	}
}

func TestDetectOSOverrideIncomplete(t *testing.T) {
	t.Setenv("TT_OVERRIDE_OS_ID", "ubuntu")
	t.Setenv("TT_OVERRIDE_OS_VERSION", "")
	if _, err := DetectOS(filepath.Join("testdata", "os-release-ubuntu.txt")); err == nil {
		t.Fatal("expected error for incomplete override, got nil")
	}
}

func TestResolveManifestKey(t *testing.T) {
	ubuntu := &OSInfo{ID: "ubuntu", VersionID: "24.04", IDLike: []string{"debian"}, UbuntuCodename: "noble"}
	mint := &OSInfo{ID: "linuxmint", VersionID: "22.1", IDLike: []string{"ubuntu", "debian"}, UbuntuCodename: "noble"}
	fedora := &OSInfo{ID: "fedora", VersionID: "40"}

	tests := []struct {
		name      string
		os        *OSInfo
		available []string
		want      string
		wantErr   bool
	}{
		{
			name:      "ubuntu exact",
			os:        ubuntu,
			available: []string{"ubuntu-24.04"},
			want:      "ubuntu-24.04",
		},
		{
			name:      "mint exact wins over parent",
			os:        mint,
			available: []string{"linuxmint-22.1", "ubuntu-24.04"},
			want:      "linuxmint-22.1",
		},
		{
			name:      "mint falls back to ubuntu via codename",
			os:        mint,
			available: []string{"ubuntu-24.04"},
			want:      "ubuntu-24.04",
		},
		{
			name:      "fedora exact",
			os:        fedora,
			available: []string{"fedora-40"},
			want:      "fedora-40",
		},
		{
			name:      "fedora does not map to ubuntu",
			os:        fedora,
			available: []string{"ubuntu-24.04"},
			wantErr:   true,
		},
		{
			name:      "no manifest available",
			os:        mint,
			available: []string{"debian-12"},
			wantErr:   true,
		},
		{
			name:      "derivative version is not reused for parent",
			os:        &OSInfo{ID: "linuxmint", VersionID: "22.1", IDLike: []string{"ubuntu", "debian"}},
			available: []string{"ubuntu-22.1"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.os.ResolveManifestKey(tt.available)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got key %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveManifestKey = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManifestCandidatesUsesVersionCodename(t *testing.T) {
	// A derivative distro that exposes only VERSION_CODENAME (no UBUNTU_CODENAME)
	// should still resolve to the parent Ubuntu manifest.
	o := &OSInfo{ID: "pop", VersionID: "22.04", IDLike: []string{"ubuntu"}, VersionCodename: "jammy"}
	got, err := o.ResolveManifestKey([]string{"ubuntu-22.04"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ubuntu-22.04" {
		t.Errorf("got %q, want ubuntu-22.04", got)
	}
}
