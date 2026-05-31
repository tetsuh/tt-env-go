// Package buildinfo exposes build-time version metadata for the tt-env binary.
//
// The exported variables are intended to be overridden at build time via the
// Go linker, for example:
//
//	go build -ldflags "-X github.com/tetsuh/tt-env-go/pkg/buildinfo.Version=0.1.0 \
//	    -X github.com/tetsuh/tt-env-go/pkg/buildinfo.Commit=$(git rev-parse HEAD) \
//	    -X github.com/tetsuh/tt-env-go/pkg/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// When the variables are not overridden the binary reports development
// defaults so an unstamped build is still clearly identifiable.
//
// This metadata is distinct from the github.com/tetsuh/tt-env-go/pkg/version
// package, which manages installed Tenstorrent stack releases.
package buildinfo

import "fmt"

var (
	// Version is the semantic version of the build (e.g. "0.1.0").
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build date, typically in RFC 3339 format.
	Date = "unknown"
)

// String returns a single-line summary of the build version metadata.
func String() string {
	return fmt.Sprintf("tt-env %s (commit %s, built %s)", Version, Commit, Date)
}
