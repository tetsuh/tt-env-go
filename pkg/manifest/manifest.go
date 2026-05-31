// Package manifest defines the typed representation of a tt-env release
// manifest and a loader that parses and validates it at startup.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// Manifest is the typed representation of a release manifest JSON file
// (e.g. releases/<release>.json).
type Manifest struct {
	Release             string                        `json:"release"`
	Description         string                        `json:"description"`
	Components          map[string]Component          `json:"components"`
	SystemPackages      map[string]string             `json:"system_packages"`
	PythonPackages      map[string]string             `json:"python_packages"`
	GitComponents       map[string]GitComponent       `json:"git_components"`
	ContainerComponents map[string]ContainerComponent `json:"container_components"`
}

// GitComponent describes a git-sourced component. Version may hold either a tag
// or a commit SHA.
type GitComponent struct {
	URL     string `json:"url"`
	Version string `json:"version"`
}

// componentSHA256Re matches a lowercase- or uppercase-hex SHA-256 digest.
var componentSHA256Re = regexp.MustCompile(`^[A-Fa-f0-9]{64}$`)

// Component describes a stack component entry. In a manifest it may be written
// either as a bare version string or as an object with a version and the
// download_url/sha256 needed when system-package installation is disabled.
type Component struct {
	Version     string
	DownloadURL string
	SHA256      string
}

// componentObject is the object form of a Component used for (un)marshalling.
type componentObject struct {
	Version     string `json:"version,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

// UnmarshalJSON accepts either a bare JSON string (interpreted as the version)
// or an object with version/download_url/sha256. Other JSON types are rejected.
func (c *Component) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("component: empty value")
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		c.Version = s
		c.DownloadURL = ""
		c.SHA256 = ""
		return nil
	case '{':
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		var obj componentObject
		if err := dec.Decode(&obj); err != nil {
			return err
		}
		c.Version = obj.Version
		c.DownloadURL = obj.DownloadURL
		c.SHA256 = obj.SHA256
		return nil
	default:
		return fmt.Errorf("component: must be a string or an object, got %s", string(trimmed))
	}
}

// MarshalJSON emits a bare string when only Version is set, preserving the
// string form of components that carry no download metadata.
func (c Component) MarshalJSON() ([]byte, error) {
	if c.DownloadURL == "" && c.SHA256 == "" {
		return json.Marshal(c.Version)
	}
	return json.Marshal(componentObject(c))
}

// ContainerComponent describes a container-sourced component. It is valid in one
// of two shapes: a Ref that points at another container component, or an
// ImageURL together with an ImageTag.
type ContainerComponent struct {
	Ref      string `json:"ref,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	ImageTag string `json:"image_tag,omitempty"`
}

// Load reads the manifest file at path, decodes it, and validates it. It
// returns a descriptive error if the file cannot be read, contains malformed
// JSON, or fails validation.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest %s: invalid JSON: %w", path, err)
	}

	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}

	return &m, nil
}

// Validate checks that the manifest contains the required fields and that each
// nested component is well-formed.
func (m *Manifest) Validate() error {
	if m.Release == "" {
		return fmt.Errorf(`missing required field "release"`)
	}

	for name, c := range m.Components {
		if c.DownloadURL != "" || c.SHA256 != "" {
			if c.DownloadURL == "" {
				return fmt.Errorf("component %q: missing required field \"download_url\"", name)
			}
			if c.SHA256 == "" {
				return fmt.Errorf("component %q: missing required field \"sha256\"", name)
			}
			if !componentSHA256Re.MatchString(c.SHA256) {
				return fmt.Errorf("component %q: invalid sha256 %q", name, c.SHA256)
			}
		}
	}

	for name, gc := range m.GitComponents {
		if gc.URL == "" {
			return fmt.Errorf("git component %q: missing required field \"url\"", name)
		}
		if gc.Version == "" {
			return fmt.Errorf("git component %q: missing required field \"version\"", name)
		}
	}

	for name, cc := range m.ContainerComponents {
		hasRef := cc.Ref != ""
		hasImage := cc.ImageURL != "" || cc.ImageTag != ""
		switch {
		case hasRef && hasImage:
			return fmt.Errorf("container component %q: must set either \"ref\" or \"image_url\"/\"image_tag\", not both", name)
		case !hasRef && !hasImage:
			return fmt.Errorf("container component %q: must set either \"ref\" or both \"image_url\" and \"image_tag\"", name)
		case hasImage && (cc.ImageURL == "" || cc.ImageTag == ""):
			return fmt.Errorf("container component %q: both \"image_url\" and \"image_tag\" are required", name)
		}
	}

	return nil
}
