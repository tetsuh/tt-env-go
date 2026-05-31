# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0]

First tagged release of `tt-env-go`, a statically compiled Go re-implementation
of the `tt-env` Tenstorrent stack environment manager. It reaches feature parity
with the Bash prototype's core workflows and ships hosted CI plus reproducible,
release-stamped binaries.

### Added

- **CLI skeleton** built on Cobra with structured logging and a `--log-level`
  flag.
- **`status`** command that probes the active release, installed releases,
  detected Tenstorrent hardware, and KMD/Secure Boot state.
- **`use`**, **`list`**, and **`remove`** commands for switching, enumerating,
  and uninstalling local stack releases via the active-version symlink and
  generated command shims.
- **`update`** command that refreshes release manifest catalogs.
- **`diff`** command that compares two release manifests, reporting version and
  dependency differences.
- **`install`** command that provisions a release from its manifest, including
  system packages through package-manager adapters, git component clones, and a
  pinned Python virtual environment; supports `--dry-run`, `--force`, and
  `--latest` (unpinned/HEAD) installation modes.
- **`capture`** command that produces a local-only stack release manifest by
  probing installed package, pip, and git component versions, with `--from` to
  decouple the probed tree from the base manifest template, and GHCR digest
  resolution to pin container components to their `latest` image digests.
- **`version`** subcommand and `--version` flag reporting the build version,
  git commit, and build date embedded at link time.
- **Continuous integration** running gofmt, `go vet`, golangci-lint,
  govulncheck, a multi-version `go test -race` matrix, and cross-compiled
  linux/amd64 and linux/arm64 builds.
- **Release automation** that builds version-stamped static binaries for
  linux/amd64 and linux/arm64 with checksums and publishes them to a GitHub
  Release on `v*` tags.

[Unreleased]: https://github.com/tetsuh/tt-env-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tetsuh/tt-env-go/releases/tag/v0.1.0
