# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`install`** resolves unpinned system and Python packages to candidates
  from configured repositories and indexes, and records the resulting versions
  in a lock at `versions/<release>/manifest.json`. `--force` on a locked release
  replays its pins; `--upgrade --force` intentionally re-resolves host tooling.
  Pinned package availability is checked before mutation against configured
  repositories and indexes (DNF uses cached metadata only, so a required
  repository not yet configured can block installation). Locks also record git
  revisions, container components, and provenance (`source`, `base`, catalog
  repository and ref, install timestamp); `list` and `status` show provenance,
  and `diff` resolves an installed release to its lock.
- **`update`** records the fetched catalog's provenance (repository and ref) in
  `manifests/catalog_source.json`, cited by install-time locks.

### Changed

- **`install --upgrade`** replaces `--latest` (kept as a hidden deprecated alias):
  it re-resolves host system/Python packages and git HEAD, but retains container
  references from the template manifest. `--like` replaces `--base` (alias kept)
  for choosing that template. Both dry-run and real installs summarize the
  resolution scope and explain skipped optional packages; a missing manifest
  lists available releases and suggests `--upgrade --like`.

### Fixed

- **`update`** no longer deletes locally captured release manifests: manifests
  in `releases/` that the fetched catalog does not carry are moved to
  `releases.local/`. Captured manifests are now written to `releases.local/`,
  and manifest lookups (`install`, `capture --base`, `diff`, `list`) prefer a
  local manifest over a catalog manifest with the same release name (`list`
  includes `local` in the bracketed status).

## [0.1.1] - 2026-08-15

### Changed

- **`update`** now fetches manifests from the official public
  `tetsuh/tt-env-manifests` catalog by default instead of the prototype catalog.

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

[Unreleased]: https://github.com/tetsuh/tt-env-go/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/tetsuh/tt-env-go/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/tetsuh/tt-env-go/releases/tag/v0.1.0
