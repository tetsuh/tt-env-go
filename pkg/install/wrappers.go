package install

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed templates/git_wrapper.sh.tmpl templates/container_wrapper.sh.tmpl templates/python_wrapper.sh.tmpl
var templatesFS embed.FS

var (
	gitWrapperTemplate       = template.Must(template.ParseFS(templatesFS, "templates/git_wrapper.sh.tmpl"))
	containerWrapperTemplate = template.Must(template.ParseFS(templatesFS, "templates/container_wrapper.sh.tmpl"))
	pythonWrapperTemplate    = template.Must(template.ParseFS(templatesFS, "templates/python_wrapper.sh.tmpl"))
)

// defaultEntrypoint is the git component entrypoint used when a manifest does
// not specify one. It matches proto1's default of run.py.
const defaultEntrypoint = "run.py"

// componentNameRe constrains component (and therefore bin/<name> wrapper)
// filenames to a single safe path element, preventing path traversal and shell
// metacharacter injection when the name is embedded in a wrapper script.
var componentNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// imageRefRe constrains a container image reference to characters that are safe
// to embed in a double-quoted shell string. It rejects whitespace, quotes, and
// shell metacharacters.
var imageRefRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)

// validateComponentName ensures name is a single safe path element.
func validateComponentName(name string) error {
	if name == "." || name == ".." || !componentNameRe.MatchString(name) {
		return fmt.Errorf("install: invalid component name: %q", name)
	}
	return nil
}

// validateImageRef ensures ref is safe to embed in a shell wrapper.
func validateImageRef(ref string) error {
	if !imageRefRe.MatchString(ref) {
		return fmt.Errorf("install: invalid container image reference: %q", ref)
	}
	return nil
}

// gitWrapperData holds the substitutions for the git component wrapper.
type gitWrapperData struct {
	Component  string
	Entrypoint string
	VenvSubdir string
}

// containerWrapperData holds the substitutions for the container component
// wrapper.
type containerWrapperData struct {
	Component    string
	ImageRef     string
	IsTTMetalium bool
}

// renderGitWrapper renders the git component wrapper script.
func renderGitWrapper(component, entrypoint, venvSubdir string) ([]byte, error) {
	if err := validateComponentName(component); err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := gitWrapperTemplate.Execute(&buf, gitWrapperData{
		Component:  component,
		Entrypoint: entrypoint,
		VenvSubdir: venvSubdir,
	}); err != nil {
		return nil, fmt.Errorf("install: render git wrapper for %q: %w", component, err)
	}
	return []byte(buf.String()), nil
}

// renderContainerWrapper renders the container component wrapper script.
func renderContainerWrapper(component, imageRef string) ([]byte, error) {
	if err := validateComponentName(component); err != nil {
		return nil, err
	}
	if err := validateImageRef(imageRef); err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := containerWrapperTemplate.Execute(&buf, containerWrapperData{
		Component:    component,
		ImageRef:     imageRef,
		IsTTMetalium: component == "tt-metalium",
	}); err != nil {
		return nil, fmt.Errorf("install: render container wrapper for %q: %w", component, err)
	}
	return []byte(buf.String()), nil
}

// writeWrapper writes an executable wrapper script to binDir/<component>.
func writeWrapper(binDir, component string, content []byte) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("install: create bin directory: %w", err)
	}
	path := filepath.Join(binDir, component)
	if err := os.WriteFile(path, content, 0o755); err != nil {
		return fmt.Errorf("install: write wrapper %s: %w", path, err)
	}
	return nil
}

// pythonWrapperData holds the substitutions for the python command wrapper.
// Exactly one of VenvCommandName or TargetCommand is set: VenvCommandName for a
// venv-provided command, TargetCommand (an absolute, shell-quoted path) for a
// system command shimmed through the venv python.
type pythonWrapperData struct {
	VenvSubdir      string
	VenvCommandName string
	TargetCommand   string
}

// shellSingleQuote wraps s in single quotes, escaping any embedded single
// quotes, so it is safe to embed verbatim in a shell script.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// renderVenvPythonWrapper renders a python wrapper that targets a command
// provided by the release virtualenv.
func renderVenvPythonWrapper(command, venvSubdir string) ([]byte, error) {
	if err := validateComponentName(command); err != nil {
		return nil, err
	}
	return renderPythonWrapper(pythonWrapperData{
		VenvSubdir:      venvSubdir,
		VenvCommandName: shellSingleQuote(command),
	})
}

// renderAbsolutePythonWrapper renders a python wrapper that targets an absolute
// system command path through the release virtualenv.
func renderAbsolutePythonWrapper(targetPath, venvSubdir string) ([]byte, error) {
	return renderPythonWrapper(pythonWrapperData{
		VenvSubdir:    venvSubdir,
		TargetCommand: shellSingleQuote(targetPath),
	})
}

func renderPythonWrapper(data pythonWrapperData) ([]byte, error) {
	var buf strings.Builder
	if err := pythonWrapperTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("install: render python wrapper: %w", err)
	}
	return []byte(buf.String()), nil
}

// containerImageRef builds the image reference from a URL and tag, using the
// digest form (url@tag) when the tag is a sha256 digest and the tagged form
// (url:tag) otherwise, mirroring proto1.
func containerImageRef(imageURL, imageTag string) string {
	if strings.HasPrefix(imageTag, "sha256:") {
		return imageURL + "@" + imageTag
	}
	return imageURL + ":" + imageTag
}
