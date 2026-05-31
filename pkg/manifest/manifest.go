// Package manifest defines the typed representation of a tt-env release
// manifest and a loader that parses and validates it at startup.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest is the typed representation of a release manifest JSON file
// (e.g. releases/<release>.json).
type Manifest struct {
	Release             string                        `json:"release"`
	Description         string                        `json:"description"`
	Components          map[string]string             `json:"components"`
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
