package install

import (
	"strings"
	"testing"
)

func TestRenderGitWrapper(t *testing.T) {
	content, err := renderGitWrapper("tt-foo", "run.py", "venv")
	if err != nil {
		t.Fatalf("renderGitWrapper: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"VENV_DIR=\"${VERSION_DIR}/venv\"",
		"PYTHONPATH=\"${VERSION_DIR}/src/tt-foo${PYTHONPATH:+:${PYTHONPATH}}\"",
		"TARGET_COMMAND=\"${VERSION_DIR}/src/tt-foo/run.py\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("git wrapper missing %q\n%s", want, got)
		}
	}
	assertNoTemplateLeftovers(t, got)
}

func TestRenderContainerWrapperTaggedAndDigest(t *testing.T) {
	tagged, err := renderContainerWrapper("tt-metalium-models", "ghcr.io/x/y:1.2.3")
	if err != nil {
		t.Fatalf("renderContainerWrapper tagged: %v", err)
	}
	if !strings.Contains(string(tagged), `COMPONENT_IMAGE="ghcr.io/x/y:1.2.3"`) {
		t.Errorf("tagged image not embedded:\n%s", tagged)
	}
	assertNoTemplateLeftovers(t, string(tagged))

	// tt-metalium gets the extra Ubuntu-version NOTE line.
	metal, err := renderContainerWrapper("tt-metalium", "ghcr.io/x/y@sha256:deadbeef")
	if err != nil {
		t.Fatalf("renderContainerWrapper metalium: %v", err)
	}
	if !strings.Contains(string(metal), "use tt-metalium-ubuntu22") {
		t.Errorf("tt-metalium wrapper missing Ubuntu NOTE line")
	}

	// Non-metalium component must not include the NOTE line.
	other, _ := renderContainerWrapper("tt-metalium-models", "ghcr.io/x/y:1.2.3")
	if strings.Contains(string(other), "use tt-metalium-ubuntu22") {
		t.Errorf("non-metalium wrapper should not include the Ubuntu NOTE line")
	}
}

func TestContainerImageRef(t *testing.T) {
	if got := containerImageRef("ghcr.io/x/y", "1.2.3"); got != "ghcr.io/x/y:1.2.3" {
		t.Errorf("tagged ref = %q", got)
	}
	if got := containerImageRef("ghcr.io/x/y", "sha256:abc"); got != "ghcr.io/x/y@sha256:abc" {
		t.Errorf("digest ref = %q", got)
	}
}

func TestValidateComponentNameRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "../x", "a/b", "a;b", "a b", "$(x)"} {
		if err := validateComponentName(bad); err == nil {
			t.Errorf("expected error for component name %q", bad)
		}
	}
	for _, ok := range []string{"tt-smi", "tt-metalium-ubuntu24", "a.b_c-1"} {
		if err := validateComponentName(ok); err != nil {
			t.Errorf("unexpected error for component name %q: %v", ok, err)
		}
	}
}

func TestValidateImageRefRejectsMetacharacters(t *testing.T) {
	for _, bad := range []string{"", "a b", "a;rm -rf", "$(x)", "a`b`", "a\"b"} {
		if err := validateImageRef(bad); err == nil {
			t.Errorf("expected error for image ref %q", bad)
		}
	}
	for _, ok := range []string{"ghcr.io/x/y:1.2.3", "ghcr.io/x/y@sha256:deadbeef"} {
		if err := validateImageRef(ok); err != nil {
			t.Errorf("unexpected error for image ref %q: %v", ok, err)
		}
	}
}

func TestRenderRejectsBadInputs(t *testing.T) {
	if _, err := renderGitWrapper("../x", "run.py", "venv"); err == nil {
		t.Error("expected git wrapper render to reject bad component name")
	}
	if _, err := renderContainerWrapper("ok", "bad ref"); err == nil {
		t.Error("expected container wrapper render to reject bad image ref")
	}
}

func assertNoTemplateLeftovers(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "{{") {
		t.Errorf("rendered script contains unrendered template markers:\n%s", s)
	}
}
