# Contributing

Bug reports and pull requests are welcome. For anything larger than a fix,
open an issue first so the change can be agreed on before it is written.

## Development

You need Go (the version in `go.mod`) and
[golangci-lint](https://golangci-lint.run) v2.

```sh
make build   # ./bin/goship
make check   # lint, race tests and govulncheck — what CI runs
```

Point a build at another API with `GOSHIP_API`, and keep it off your real
config with `GOSHIP_CONFIG=/tmp/goship.json`.

## Pull requests

- Keep each one to a single change, with tests for what it fixes or adds.
  Command tests run the real cobra tree against an `httptest` server; see
  `internal/cli/auth_test.go` for the helpers.
- `make check` must pass.
- Commit messages are one line in the conventional style: `fix: …`,
  `feat: …`, `refactor: …`.
- [docs/architecture.md](docs/architecture.md) has the rules the code follows —
  above all, commands hand values to `render` rather than printing them.

By contributing you agree that your contribution is licensed under the
[Apache License 2.0](LICENSE).
