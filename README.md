# tt-env-go

A statically compiled Go implementation of **tt-env** — an environment manager
for the Tenstorrent software stack.

This repository is the Go successor to the Bash prototype `tt-env-proto1`. It
targets frictionless cross-compilation to `linux/amd64` and `linux/arm64`,
distributed as self-contained static binaries.

## Status

🚧 Work in progress. Development is tracked through the GitHub
[Issues](https://github.com/tetsuh/tt-env-go/issues) and
[Milestones](https://github.com/tetsuh/tt-env-go/milestones) of this repository.

The CLI skeleton (Milestone 1) is in place: a Cobra root command, structured
logging, and the `install`, `remove`, `use`, `list`, `status`, `update`, and
`diff` subcommands.

## Layout

```text
tt-env-go/
├── cmd/
│   └── tt-env/          # CLI entrypoint
└── pkg/
    ├── cli/             # Cobra command parser
    ├── logger/          # Structured logging (slog)
    ├── buildinfo/       # Build version metadata (set via -ldflags)
    ├── manifest/        # Release JSON schema & OS parsing
    ├── catalog/         # Catalog vs. local manifest lookup (releases.local/)
    ├── lock/            # Install-time lock (versions/<release>/manifest.json)
    ├── package_manager/ # Apt / Dnf adapters
    ├── version/         # Stack release install / use / list / remove
    ├── shims/           # Wrapper & shim generator
    ├── kmd/             # Kernel module preflights & safe swaps
    └── status/          # Hardware (lspci) & environment probing
```

## Build

Requires Go 1.23 or newer.

```bash
go build ./...          # build all packages
go build -o tt-env ./cmd/tt-env
```

## Manifest locations

`tt-env` keeps release manifests in two places under `TT_HOME`:

- `releases/` — the catalog cache fetched by `tt-env update`, replaced
  wholesale on every update.
- `releases.local/` — user-authored local manifests (e.g. `tt-env capture`).
  `tt-env update` never replaces or deletes files here; it only adds migrated
  manifests and archives conflicting copies under `releases.local/conflicts/`.

Manifest lookups (`install`, `capture --base`, `diff`, `list`) search both
locations; a local manifest overrides a catalog manifest with the same release
name. `tt-env update` moves any file in `releases/` that the fetched catalog
does not carry into `releases.local/` instead of deleting it, and records the
fetched catalog's provenance (repository and ref) in
`manifests/catalog_source.json`.

## Install-time locks

A catalog manifest states *intent* — which packages and versions an install
should resolve. An ordinary catalog install resolves unpinned system and Python
packages to the candidates available from configured repositories and indexes;
every install records the resulting resolution in a lock at
`versions/<release>/manifest.json`, written during staging so it appears
atomically with the release. For an already-installed release, `--force` replays
the lock's pins; use `--latest --force` to intentionally refresh package
versions.

Before mutation, pinned system and Python package versions are checked against
the currently configured repositories and indexes. DNF checks cached metadata
only, and a required repository that is not yet configured can make a pinned
version appear unavailable and block installation.

The lock records:

- concrete system-package and Python versions (pinned versions and resolved
  candidates),
- git components at their resolved revisions (remote HEAD for `--latest`),
- the container components as installed, and
- provenance: `source` (`catalog` | `local` | `latest`), the `base` for a
  `--latest` install, the catalog repository and ref when known, and the
  install timestamp.

`tt-env list` and `tt-env status` show this provenance. For example, `list`
prints `2026.05.16 (from catalog tetsuh/tt-env-manifests@main, resolved
2026-09-23) [installed]`; the `[installed]` marker is specific to `list` (the
`status` command shows its own installed-release summary). `tt-env diff`
resolves an installed release to its lock, so a diff can compare the actually
installed versions against catalog intent.
Releases installed before locks existed simply have no lock and fall back to
the manifest catalog.

## Releases

Tagged releases (`v*`) are published automatically by the
[release workflow](.github/workflows/release.yml): it cross-compiles
version-stamped static binaries for `linux/amd64` and `linux/arm64`, generates
`checksums.txt`, and attaches them to a
[GitHub Release](https://github.com/tetsuh/tt-env-go/releases). Release notes are
derived from [`CHANGELOG.md`](CHANGELOG.md).

Each binary embeds its build metadata, viewable with:

```bash
tt-env version          # tt-env <version> (commit <sha>, built <date>)
tt-env --version
```

## Verification

Run the standard Go toolchain before opening a pull request:

```bash
gofmt -l .              # must report no files
go vet ./...
go build ./...
go test ./...
```

## Contributing

Contributions are coordinated through GitHub Issues and pull requests. Commit
messages and PR titles must follow
[Conventional Commits](https://www.conventionalcommits.org/) (e.g.
`feat(cli): add status command`). See [`AGENTS.md`](./AGENTS.md) for the full
workflow, including branch naming, PR, and review conventions.

## License

Apache License 2.0 — see [`LICENSE`](./LICENSE).
