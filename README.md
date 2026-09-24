# goship

CLI for [GoShip](https://goship.sh) — deploy and operate apps, databases and
your own nodes.

```sh
$ goship deploy
Deploying api · go

  ✓ Create        go, detected from go.mod, plan Free   0.4s
  ✓ Environment   3 variables                           0.2s
  ✓ Upload        48.2 KB                               0.3s
  ✓ Build         image v1                              41s
  ✓ Release       1/1 units healthy                     9.8s
  ✓ Route                                               0.4s

  → https://api.apps.goship.sh   v1 · 53s
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
  ✓ Create        go, detected from go.mod, plan Free   0.4s
```

A plan is only chosen for you when it is free. If your organization has no free
grant, the command stops and shows what the options cost — `goship plans` lists
them — rather than spending your money on a guess.

With no platform marker but a `Dockerfile` (or `Containerfile`) present, the
container file builds the image instead:

```
Building widget from Dockerfile.
```

Such an app is created with no platform at all; the image the container file
builds is the whole definition. A project with both a marker and a Dockerfile is
built by its platform — a Go service that ships a Dockerfile is still a Go
service. Pass `--dockerfile` to override that.

Flags override the rest too:

```sh
goship deploy --platform python --plan app-small-sandboxed
```

A `goship.yml` in the directory is an optional shortcut for the same values, so
you stop retyping them. `goship init` writes one, filling in what it can work
out from the directory and leaving the rest for you:

```yaml
app: api
org: acme
platform: go
plan: app-small-sandboxed
env:
  LOG_LEVEL: info
```

To run it on a machine you own, name the node — `goship nodes` shows them.
Nothing on your own hardware is billed, so plans there are just sizes;
`goship plans` lists them per placement, with the node's pool default marked:

```
$ goship plans

GoShip (shared)
SLUG                    NAME    CPU   MEMORY   PRICE
app-micro-sandboxed     Micro   200   256      R$ 9.90/mo
app-small-sandboxed     Small   500   512      R$ 19.90/mo

do-server1 (your hardware — not billed)
SLUG                       NAME                CPU    MEMORY   PRICE
byon-do-server1-default    Sized to the node   1800   3600     included   default
app-small-sandboxed        Small               500    512      included
```

`goship deploy --node do-server1` with no plan takes the pool's default; `--plan`
picks any size the pool admits, including the one sized to the whole node.

`org` pins the organization for the whole project, so a repo always deploys to
the same place regardless of what `goship org use` last selected. It applies to
every command run inside the directory, not only `deploy`. `--org` still wins.

Flags beat the file; the file beats what is inferred. Secrets belong in a
dotenv file instead, applied as private variables:

```sh
goship deploy --env-file .env
```

## Commands

| | |
|---|---|
| `goship apps\|dbs\|domains\|volumes\|nodes [-A]` | list in the current org; `--all`, or no org selected, spans every org |
| `goship app create\|info\|rm\|start\|stop\|restart\|scale\|plan` | manage one app |
| `goship init [dir]` | write a `goship.yml` with what it can work out |
| `goship deploy [dir]` | create if needed, apply env, deploy |
| `goship logs -a api [-f]` | stream logs |
| `goship run -a api -- <cmd>` | run a command in the app's containers |
| `goship shell -a api` | shell into a unit |
| `goship env list\|set\|unset -a api` | environment variables |
| `goship db create\|info\|users\|user-add\|bind\|unbind\|rm` | managed PostgreSQL |
| `goship domain add\|rm\|register\|verify` | custom domains and TLS |
| `goship volume create\|info\|bind\|unbind\|rm` | persistent disks |
| `goship node add\|info\|rm` | your own machines (BYON) |
| `goship plans [--node id]` | what you can choose, per placement: priced on GoShip, sizes on your nodes |
| `goship releases` / `goship rollback` | deploy history |
| `goship orgs` / `goship org use <slug>` | list orgs / choose one |
| `goship tokens` / `goship token create\|rm` | list / manage API tokens for CI |

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

Commands that stream — `deploy`, `logs`, `run`, `shell`, `rollback` — reject
`--output json` rather than pretend to support it.

`deploy` and `rollback` show their steps; when one fails, its whole output is
printed. `--verbose` (`-v`) shows the complete log as it arrives instead, and
works on every command (it also logs HTTP requests to stderr). Output that is
not a terminal gets one line per step, without colour or redraws.

## Configuration

| | |
|---|---|
| `GOSHIP_API` | GoShip API base URL |
| `GOSHIP_ORG` | organization slug |
| `GOSHIP_TOKEN` | bearer token |
| `GOSHIP_CONFIG` | config file path |

Defaults live in `~/.config/goship/config.json`, written `0600` because it
holds a token. Flags beat environment variables, which beat the file. The
variables apply to the process that sets them and are never written to the
file, so a CI token stays in CI.

## Development

```sh
make build   # ./bin/goship
make check   # lint, race tests and govulncheck, as CI runs them
```

How the CLI is put together is in [docs/architecture.md](docs/architecture.md).
See [CONTRIBUTING.md](CONTRIBUTING.md) to send a change and
[SECURITY.md](SECURITY.md) to report a vulnerability.

## License

Apache 2.0. See [LICENSE](LICENSE).
