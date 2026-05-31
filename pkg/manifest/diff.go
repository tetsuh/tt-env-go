package manifest

import (
	"fmt"
	"io"
	"sort"
)

// DiffRow is a single side-by-side comparison line between two manifests for a
// given item. Left or Right holds "-" when the item is absent on that side.
type DiffRow struct {
	Item  string
	Left  string
	Right string
}

// DiffResult holds the side-by-side comparison of two manifests.
type DiffResult struct {
	LeftRelease  string
	RightRelease string
	Rows         []DiffRow
}

// Diff computes a side-by-side comparison of two manifests, mirroring proto1
// diff_releases. It compares components, system_packages, python_packages,
// git_components, and container_components. Every item present on either side is
// included, even when unchanged.
func Diff(left, right *Manifest) DiffResult {
	res := DiffResult{
		LeftRelease:  left.Release,
		RightRelease: right.Release,
	}
	appendStringSection(&res.Rows, "components",
		componentVersionStrings(left.Components), componentVersionStrings(right.Components))
	appendStringSection(&res.Rows, "system_packages", left.SystemPackages, right.SystemPackages)
	appendStringSection(&res.Rows, "python_packages", left.PythonPackages, right.PythonPackages)
	appendStringSection(&res.Rows, "git_components",
		gitComponentStrings(left.GitComponents), gitComponentStrings(right.GitComponents))
	appendStringSection(&res.Rows, "container_components",
		containerComponentStrings(left.ContainerComponents), containerComponentStrings(right.ContainerComponents))
	return res
}

// Render writes the diff as a fixed-width, side-by-side table to w, mirroring
// the proto1 diff column layout.
func (d DiffResult) Render(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%-32s %-24s %-24s\n", "Item", d.LeftRelease, d.RightRelease); err != nil {
		return err
	}
	for _, row := range d.Rows {
		if _, err := fmt.Fprintf(w, "%-32s %-24s %-24s\n", row.Item, row.Left, row.Right); err != nil {
			return err
		}
	}
	return nil
}

// appendStringSection appends one row per key in the union of left and right,
// sorted by key, prefixing each item with prefix. Absent values render as "-".
func appendStringSection(rows *[]DiffRow, prefix string, left, right map[string]string) {
	for _, key := range sortedUnionKeys(left, right) {
		*rows = append(*rows, DiffRow{
			Item:  prefix + "." + key,
			Left:  valueOrDash(left, key),
			Right: valueOrDash(right, key),
		})
	}
}

// valueOrDash returns m[key] when present and non-empty, otherwise "-",
// matching proto1's ${value:--} display semantics.
func valueOrDash(m map[string]string, key string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return "-"
}

// sortedUnionKeys returns the sorted union of the keys of left and right.
func sortedUnionKeys(left, right map[string]string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for k := range left {
		seen[k] = struct{}{}
	}
	for k := range right {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// componentVersionStrings flattens components into a comparable string keyed by
// component name. Components without download metadata render as their bare
// version (preserving the prior diff output); components that declare a download
// URL or checksum include that metadata so source/integrity changes are visible
// even when the version is unchanged.
func componentVersionStrings(comps map[string]Component) map[string]string {
	if comps == nil {
		return nil
	}
	out := make(map[string]string, len(comps))
	for name, c := range comps {
		if c.DownloadURL == "" && c.SHA256 == "" {
			out[name] = c.Version
			continue
		}
		out[name] = fmt.Sprintf("%s download_url=%s sha256=%s", c.Version, c.DownloadURL, c.SHA256)
	}
	return out
}

// gitComponentStrings flattens git components into a comparable "url@version"
// representation keyed by component name.
func gitComponentStrings(comps map[string]GitComponent) map[string]string {
	if comps == nil {
		return nil
	}
	out := make(map[string]string, len(comps))
	for name, gc := range comps {
		out[name] = formatGitComponent(gc)
	}
	return out
}

// formatGitComponent renders a git component as "url@version", or just the
// version when no URL is set.
func formatGitComponent(gc GitComponent) string {
	if gc.URL == "" {
		return gc.Version
	}
	return fmt.Sprintf("%s@%s", gc.URL, gc.Version)
}

// containerComponentStrings flattens container components into a comparable
// representation keyed by component name.
func containerComponentStrings(comps map[string]ContainerComponent) map[string]string {
	if comps == nil {
		return nil
	}
	out := make(map[string]string, len(comps))
	for name, cc := range comps {
		out[name] = formatContainerComponent(cc)
	}
	return out
}

// formatContainerComponent renders a container component as "ref:<ref>" when it
// references another component, otherwise as "image_url=<url> image_tag=<tag>".
// The explicit key=value form avoids ambiguity when image_tag is a digest such
// as "sha256:...".
func formatContainerComponent(cc ContainerComponent) string {
	if cc.Ref != "" {
		return "ref:" + cc.Ref
	}
	return fmt.Sprintf("image_url=%s image_tag=%s", cc.ImageURL, cc.ImageTag)
}
