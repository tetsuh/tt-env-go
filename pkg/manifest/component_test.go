package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestComponentUnmarshalStringForm(t *testing.T) {
	var c Component
	if err := json.Unmarshal([]byte(`"v1.2.3"`), &c); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if c.Version != "v1.2.3" || c.DownloadURL != "" || c.SHA256 != "" {
		t.Errorf("got %+v", c)
	}
}

func TestComponentUnmarshalObjectForm(t *testing.T) {
	sha := strings.Repeat("a", 64)
	var c Component
	in := `{"version":"v1","download_url":"https://example.test/a","sha256":"` + sha + `"}`
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if c.Version != "v1" || c.DownloadURL != "https://example.test/a" || c.SHA256 != sha {
		t.Errorf("got %+v", c)
	}
}

func TestComponentUnmarshalRejectsBadTypes(t *testing.T) {
	for _, in := range []string{`null`, `123`, `true`, `["x"]`} {
		var c Component
		if err := json.Unmarshal([]byte(in), &c); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}

func TestComponentUnmarshalRejectsUnknownField(t *testing.T) {
	var c Component
	if err := json.Unmarshal([]byte(`{"version":"v1","extra":"x"}`), &c); err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestComponentMarshalRoundTrip(t *testing.T) {
	// String form when only Version is set.
	b, err := json.Marshal(Component{Version: "v1"})
	if err != nil || string(b) != `"v1"` {
		t.Fatalf("marshal string form = %s, %v", b, err)
	}
	// Object form when download metadata is present.
	sha := strings.Repeat("b", 64)
	b, err = json.Marshal(Component{Version: "v1", DownloadURL: "https://x/y", SHA256: sha})
	if err != nil {
		t.Fatalf("marshal object form: %v", err)
	}
	var c Component
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if c.DownloadURL != "https://x/y" || c.SHA256 != sha {
		t.Errorf("round trip lost data: %+v", c)
	}
}

func TestValidateComponentDownloadMetadata(t *testing.T) {
	sha := strings.Repeat("c", 64)
	cases := []struct {
		name    string
		comp    Component
		wantErr bool
	}{
		{"string form", Component{Version: "v1"}, false},
		{"complete object", Component{Version: "v1", DownloadURL: "https://x/y", SHA256: sha}, false},
		{"missing sha", Component{Version: "v1", DownloadURL: "https://x/y"}, true},
		{"missing url", Component{Version: "v1", SHA256: sha}, true},
		{"invalid sha", Component{Version: "v1", DownloadURL: "https://x/y", SHA256: "nothex"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Release: "r", Components: map[string]Component{"x": tc.comp}}
			err := m.Validate()
			if tc.wantErr != (err != nil) {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
