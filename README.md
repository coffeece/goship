# goship

CLI for [GoShip](https://goship.sh) — deploy and operate apps, databases and
your own nodes.

```sh
$ goship deploy
Creating app api (go)...
Applying 3 environment variable(s)...
---- Building application image ----
---- Starting 1 new unit ----
✓ https://api.apps.goship.sh
```

## Install

```sh
go install github.com/coffeece/goship/cmd/goship@latest
```

## Getting started

```sh
goship login                 # opens the browser; one login covers every command
goship org use acme          # pick the organization to work in
goship deploy                # create + configure + deploy the current directory
```

`goship deploy` needs no configuration. The app is named after the directory,
its platform is inferred from the files present (`go.mod`, `pyproject.toml`,
`requirements.txt`, `package.json`, `index.html`), and it is created on the
first deploy — the output says what it inferred:

```
Creating app widget (go, detected from go.mod, plan Free)...
```

A plan is only chosen for you when it is free. If your organization has no free
grant, the command stops and shows what the options cost — `goship plans` lists
them — rather than spending your money on a guess.

Flags override that:

```sh
goship deploy --platform python --plan app-small-sandboxed
```

A `goship.yml` in the directory is an optional shortcut for the same values, so
you stop retyping them. `goship init` writes one, filling in what it can work
out from the directory and leaving the rest for you:

```yaml
app: api
platform: go
plan: app-small-sandboxed
env:
  LOG_LEVEL: info
```

Flags beat the file; the file beats what is inferred. Secrets belong in a
dotenv file instead, applied as private variables:

```sh
goship deploy --env-file .env
```

## Commands

| | |
|---|---|
| `goship apps` | list your apps |
| `goship app create\|info\|rm\|start\|stop\|restart\|scale\|plan` | manage one app |
| `goship init [dir]` | write a `goship.yml` with what it can work out |
| `goship deploy [dir]` | create if needed, apply env, deploy |
| `goship logs -a api [-f]` | stream logs |
| `goship run -a api -- <cmd>` | run a command in the app's containers |
| `goship shell -a api` | shell into a unit |
| `goship env list\|set\|unset -a api` | environment variables |
| `goship db create\|list\|info\|users\|bind\|unbind\|rm` | managed PostgreSQL |
| `goship domain add\|rm\|list\|register\|verify` | custom domains and TLS |
| `goship volume create\|list\|info\|bind\|unbind\|rm` | persistent disks |
| `goship node add\|list\|info\|rm` | your own machines (BYON) |
| `goship plans` | what your organization can choose |
| `goship releases` / `goship rollback` | deploy history |
| `goship org list\|use` | switch organization |

Run `goship <command> --help` for flags.

## Scripting and agents

Every command that returns data takes `--output json`:

```sh
goship apps --output json | jq -r '.[] | select(.status=="running") | .name'
```

`GOSHIP_TOKEN` skips the login prompt and `--yes` skips confirmations, so the
CLI works unattended:

```sh
GOSHIP_TOKEN=$TOKEN goship app rm old-api --yes
```

Commands that stream — `deploy`, `logs`, `run`, `shell`, `releases`,
`rollback` — reject `--output json` rather than pretend to support it.

## Configuration

| | |
|---|---|
| `GOSHIP_API` | GoShip API base URL |
| `GOSHIP_ORG` | organization slug |
| `GOSHIP_TOKEN` | bearer token |
| `GOSHIP_CONFIG` | config file path |

Defaults live in `~/.config/goship/config.json`, written `0600` because it
holds a token. Flags beat environment variables, which beat the file.

## Development

```sh
make build   # ./bin/goship
make test
make lint
```

The architecture, and why this replaced the `coffeece` CLI, is in
[docs/design/cli-architecture.md](docs/design/cli-architecture.md).

## License

BSD 3-Clause. See [LICENSE](LICENSE).
