package manifest

import "testing"

func TestParseOSManifestUbuntu(t *testing.T) {
	m, err := ParseOSManifest(readFixture(t, "manifest-ubuntu-24.04.env"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := m.PackageManager(); got != "apt" {
		t.Errorf("PackageManager() = %q, want apt", got)
	}
	if !m.UseSystemPackages() {
		t.Error("UseSystemPackages() = false, want true")
	}

	repos := m.RequiredRepos()
	if len(repos) != 1 || repos[0] != "https://ppa.tenstorrent.com/ubuntu/" {
		t.Errorf("RequiredRepos() = %v, want [https://ppa.tenstorrent.com/ubuntu/]", repos)
	}

	if got, ok := m.ResolvePackage("cmake"); !ok || got != "cmake" {
		t.Errorf("ResolvePackage(cmake) = %q, %v; want cmake, true", got, ok)
	}
	if got, ok := m.ResolvePackage("ninja"); !ok || got != "ninja-build" {
		t.Errorf("ResolvePackage(ninja) = %q, %v; want ninja-build, true", got, ok)
	}
	// Case-insensitive and separator normalization (kmd -> VIRT_PKG_KMD).
	if got, ok := m.ResolvePackage("KMD"); !ok || got != "tenstorrent-dkms" {
		t.Errorf("ResolvePackage(KMD) = %q, %v; want tenstorrent-dkms, true", got, ok)
	}
	if _, ok := m.ResolvePackage("nonexistent"); ok {
		t.Error("ResolvePackage(nonexistent) = ok, want not found")
	}

	// WORKAROUNDS is an empty array and must be present but empty.
	if workarounds, ok := m.Lists["WORKAROUNDS"]; !ok || len(workarounds) != 0 {
		t.Errorf("Lists[WORKAROUNDS] = %v, %v; want empty present", workarounds, ok)
	}
}

func TestParseOSManifestArrays(t *testing.T) {
	input := []byte(`EMPTY=()
INLINE=("a" b "c")
MULTI=(
  "one"
  two
  # comment inside array
  three
)
`)
	m, err := ParseOSManifest(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := m.Lists["EMPTY"]; len(got) != 0 {
		t.Errorf("EMPTY = %v, want empty", got)
	}
	if got := m.Lists["INLINE"]; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("INLINE = %v, want [a b c]", got)
	}
	if got := m.Lists["MULTI"]; len(got) != 3 || got[0] != "one" || got[1] != "two" || got[2] != "three" {
		t.Errorf("MULTI = %v, want [one two three]", got)
	}
}

func TestParseOSManifestRejectsUnsafeLines(t *testing.T) {
	cases := map[string]string{
		"command substitution": "PKG_MANAGER=\"$(whoami)\"",
		"backtick":             "PKG_MANAGER=\"`id`\"",
		"semicolon":            "PKG_MANAGER=\"apt; rm\"",
		"backslash":            "PKG_MANAGER=\"a\\b\"",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseOSManifest([]byte(line + "\n")); err == nil {
				t.Errorf("ParseOSManifest(%q) = nil error, want error", line)
			}
		})
	}
}

func TestParseOSManifestRejectsInvalidSyntax(t *testing.T) {
	cases := map[string]string{
		"missing quotes":     "PKG_MANAGER=apt",
		"lowercase key":      "pkg_manager=\"apt\"",
		"unterminated array": "REPOS=(\n  \"a\"\n",
		"bad scalar value":   "KEY=\"has space\"",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseOSManifest([]byte(input + "\n")); err == nil {
				t.Errorf("ParseOSManifest(%q) = nil error, want error", input)
			}
		})
	}
}
