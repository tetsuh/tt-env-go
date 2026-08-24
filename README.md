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
- `releases.local/` — user-authored local manifests (e.g. `tt-env capture`),
  never touched by `tt-env update`.

Manifest lookups (`install`, `capture --base`, `diff`, `list`) search both
locations; a local manifest overrides a catalog manifest with the same release
name. `tt-env update` moves any file in `releases/` that the fetched catalog
does not carry into `releases.local/` instead of deleting it.

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
