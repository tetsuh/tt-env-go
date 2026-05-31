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
