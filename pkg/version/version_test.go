package version

import (
	"strings"
	"testing"
)

func TestStringDefaults(t *testing.T) {
	got := String()
	want := "tt-env dev (commit none, built unknown)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStringOverrides(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origVersion, origCommit, origDate
	})

	Version = "0.1.0"
	Commit = "abc1234"
	Date = "2026-06-01T00:00:00Z"

	got := String()
	for _, want := range []string{"0.1.0", "abc1234", "2026-06-01T00:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, expected it to contain %q", got, want)
		}
	}
}
