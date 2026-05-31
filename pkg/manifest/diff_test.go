package manifest

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiffComparesAllSections(t *testing.T) {
	left := &Manifest{
		Release:        "2026.04.01",
		Components:     map[string]string{"tt-kmd": "1.0.0", "shared": "same"},
		SystemPackages: map[string]string{"kmd": "1.0.0"},
		PythonPackages: map[string]string{"tt-smi": "5.0.0"},
		GitComponents: map[string]GitComponent{
			"tt-studio": {URL: "https://example.invalid/s.git", Version: "aaa"},
		},
		ContainerComponents: map[string]ContainerComponent{
			"img": {ImageURL: "ghcr.io/x", ImageTag: "v1"},
		},
	}
	right := &Manifest{
		Release:        "2026.05.16",
		Components:     map[string]string{"tt-kmd": "2.0.0", "shared": "same", "tt-smi": "9.9"},
		SystemPackages: map[string]string{"kmd": "2.0.0"},
		PythonPackages: map[string]string{"tt-smi": "5.0.0"},
		GitComponents: map[string]GitComponent{
			"tt-studio": {URL: "https://example.invalid/s.git", Version: "bbb"},
		},
		ContainerComponents: map[string]ContainerComponent{
			"img": {Ref: "other"},
		},
	}

	d := Diff(left, right)
	if d.LeftRelease != "2026.04.01" || d.RightRelease != "2026.05.16" {
		t.Fatalf("releases = %q/%q", d.LeftRelease, d.RightRelease)
	}

	rows := make(map[string]DiffRow, len(d.Rows))
	for _, r := range d.Rows {
		rows[r.Item] = r
	}

	cases := []struct {
		item, left, right string
	}{
		{"components.tt-kmd", "1.0.0", "2.0.0"},
		{"components.shared", "same", "same"},
		{"components.tt-smi", "-", "9.9"},
		{"system_packages.kmd", "1.0.0", "2.0.0"},
		{"python_packages.tt-smi", "5.0.0", "5.0.0"},
		{"git_components.tt-studio", "https://example.invalid/s.git@aaa", "https://example.invalid/s.git@bbb"},
		{"container_components.img", "image_url=ghcr.io/x image_tag=v1", "ref:other"},
	}
	for _, c := range cases {
		got, ok := rows[c.item]
		if !ok {
			t.Errorf("missing row %q", c.item)
			continue
		}
		if got.Left != c.left || got.Right != c.right {
			t.Errorf("row %q = %q/%q, want %q/%q", c.item, got.Left, got.Right, c.left, c.right)
		}
	}
}

func TestDiffRowsSortedWithinSection(t *testing.T) {
	left := &Manifest{Release: "a", Components: map[string]string{"zeta": "1", "alpha": "1"}}
	right := &Manifest{Release: "b", Components: map[string]string{"mid": "1"}}

	d := Diff(left, right)
	var order []string
	for _, r := range d.Rows {
		order = append(order, r.Item)
	}
	want := []string{"components.alpha", "components.mid", "components.zeta"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("row order = %v, want %v", order, want)
	}
}

func TestDiffResultRender(t *testing.T) {
	d := DiffResult{
		LeftRelease:  "a",
		RightRelease: "b",
		Rows: []DiffRow{
			{Item: "components.tt-kmd", Left: "1.0.0", Right: "2.0.0"},
		},
	}
	buf := new(bytes.Buffer)
	if err := d.Render(buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 row, got %d lines: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "Item") || !strings.Contains(lines[0], "a") || !strings.Contains(lines[0], "b") {
		t.Errorf("header line = %q", lines[0])
	}
	for _, want := range []string{"components.tt-kmd", "1.0.0", "2.0.0"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row line %q missing %q", lines[1], want)
		}
	}
}

func TestFormatGitComponentWithoutURL(t *testing.T) {
	if got := formatGitComponent(GitComponent{Version: "v1"}); got != "v1" {
		t.Errorf("formatGitComponent without URL = %q, want %q", got, "v1")
	}
}

func TestDiffEmptyValueRendersDash(t *testing.T) {
	left := &Manifest{Release: "a", Components: map[string]string{"x": ""}}
	right := &Manifest{Release: "b", Components: map[string]string{"x": "1"}}

	d := Diff(left, right)
	if len(d.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(d.Rows))
	}
	if d.Rows[0].Left != "-" || d.Rows[0].Right != "1" {
		t.Errorf("row = %q/%q, want -/1", d.Rows[0].Left, d.Rows[0].Right)
	}
}
