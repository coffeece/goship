# goship

CLI for [GoShip](https://goship.sh) — deploy and operate apps, databases and
your own clusters.

```sh
$ goship deploy
Deploying api · go
Deploying to acme/web/development

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
curl -fsSL https://raw.githubusercontent.com/coffeece/goship/main/install.sh | sh
```

It picks the latest release for your OS and architecture, verifies the
checksum and installs to `/usr/local/bin` (or `~/.local/bin` when that is not
writable). `GOSHIP_VERSION=v0.3.0` pins a release and `GOSHIP_INSTALL_DIR`
picks the directory. Windows: download the zip from the
[releases page](https://github.com/coffeece/goship/releases/latest). With a
Go toolchain, `go install github.com/coffeece/goship/cmd/goship@latest` works
too.

## Getting started

```sh
goship login                    # opens the browser; one login covers every command
goship org use acme             # pick the organization to work in
goship project use web          # and the project, when the organization has several
goship environment use staging  # and the environment, when it is not the default
goship deploy                   # create + configure + deploy the current directory
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

`org` pins the organization for the whole project, so a repo always deploys to
the same place regardless of what `goship org use` last selected; `project`
does the same for the project inside it, and `environment` for the environment
(see below). They apply to every command run inside the directory, not only
`deploy`. `--org`, `--project` and `--environment` still win. A `node:` key
from an older `goship.yml` is ignored, with a warning.

Flags beat the file; the file beats what is inferred. Secrets belong in a
dotenv file instead, applied as private variables:

```sh
goship deploy --env-file .env
```

## Environments and clusters

Every project exists in each of your organization's environments. An
organization starts with three: `development` (the default), `staging` and
`production`, and can add more. Development is the usual place to deploy first;
staging is opt-in and stays disabled until an organization admin enables it
(`goship environment enable staging`, or Infrastructure → Environments in the
dashboard).
A disabled environment refuses new apps, databases and volumes, and the CLI
prints the API's refusal as is. An environment is where its apps run — on
GoShip's shared cluster, or on a cluster of your own (a machine, or a Kubernetes
cluster) — so staging can live on different hardware without becoming a second
project.

```
$ goship environments
SLUG          NAME          CLUSTER    DEFAULT   ENABLED
development   Development   goship     true      true
staging       Staging       goship     false     false
production    Production    goship     false     true
qa            QA            acme-vps   false     true
```

```sh
goship environment enable staging                      # opt in to staging (organization admin)
goship environment disable staging                     # refused for the default or one in use
goship environment create qa --cluster acme-vps        # every project of the organization gets it
goship deploy --environment qa                         # deploy there once
goship environment use qa                              # or make it this project's default
```

A command that acts on a project picks its environment, in order, from
`--environment`, `environment:` in the `goship.yml` of the working directory
(only when that file names no `org` or `project`, or the same ones as the
command's), `GOSHIP_ENVIRONMENT`, what `goship environment use` saved for the
project, and finally the organization's default (development unless changed).
`goship deploy` and `goship app create` say where they act before they create
anything (`Deploying to acme/web/staging`, `Creating api in acme/web/staging`).
If nothing chose an environment
and the organization's default cannot be read, commands that create or change
something stop and ask for `--environment`; listings and `goship app info` use
development and say so on stderr. `goship init` writes `environment:` into
`goship.yml` only when the one selected is not the organization's default.

Plans come from the environment's cluster. On GoShip's shared cluster they
carry a price; on a cluster of your own they are sizes, nothing there is billed
by plan, and the cluster decides which are available and which applies when you
name none:

```
$ goship plans
SLUG                    NAME    CPU   MEMORY   PRICE
app-micro-sandboxed     Micro   200   256      R$ 9.90/mo
app-small-sandboxed     Small   500   512      R$ 19.90/mo
```

During an invited trial, apps and databases on GoShip's cluster need no card
within the trial's allowance; `goship app create`, `goship app plan`,
`goship deploy` and `goship db create` say when billing starts.

`goship plans --kind database` lists database plans. Clusters belong to the
organization, and there are three ways to add one:

```sh
goship cluster create acme-vps --cloud my-account --region <region> --size <size>   # a new machine on a connected cloud account
goship cluster create acme-vps --host 203.0.113.7 --ssh-key ~/.ssh/id_ed25519       # a machine you already run, over SSH
goship cluster create acme-k8s --kubeconfig ./kubeconfig                            # a Kubernetes cluster you already run
goship cluster create acme-vps --cloud my-account --region <region> --size <size> --environment development,production   # and run those environments on it once it is active
```

`goship clusters` lists them, and `goship cluster info|sizes|resize|rm` manage
one. A cluster you create is billed monthly by size; apps on it are not billed
by plan. Cloud accounts (`goship cloud`) and database servers (`goship db server`)
belong to the organization too: they take no project.

## Commands

| | |
|---|---|
| `goship apps\|dbs\|volumes [-A]` | list in the current project and environment; `--all`, or nothing selected, spans every environment of every project as `org/project/environment` (`-o json` is an object keyed the same way) |
| `goship domains [-A]` | list the organization's domains |
| `goship app create\|info\|rm\|start\|stop\|restart\|scale\|plan\|visibility\|access` | manage one app |
| `goship app access list\|add\|reset\|rm\|token <app>` | who can open a private app |
| `goship init [dir]` | write a `goship.yml` with what it can work out |
| `goship deploy [dir]` | create if needed, apply env, deploy |
| `goship logs -a api [-f]` | stream logs |
| `goship run -a api -- <cmd>` | run a command in the app's containers |
| `goship shell -a api` | shell into a unit |
| `goship env list\|set\|unset -a api` | environment variables |
| `goship db create [--server <server>] [--size-limit <size>]\|size-limit\|info\|users\|user-add\|extensions\|extension-add\|extension-rm\|bind\|unbind\|rm` | managed PostgreSQL; `--server` places it on your own server (no plan needed) or, as `--server goship`, on GoShip's PostgreSQL (plan needed), and `size-limit` caps its size; `extension-add` turns on PostGIS, pg_trgm and friends |
| `goship db server list\|create\|tiers\|disk-limit\|retry\|delete` | PostgreSQL servers in your own Google Cloud project, shared by the organization's projects and unbilled by GoShip; `disk-limit` caps disk growth (`create --disk-limit`); `delete --force` forgets one GoShip can no longer reach |
| `goship domain add\|rm\|register\|verify` | custom domains and TLS |
| `goship volume create\|info\|bind\|unbind\|rm` | persistent disks, on a cluster of your own |
| `goship environments` / `goship environment use\|create\|enable\|disable\|rm <slug>` | list the organization's environments, the cluster each runs on and whether it is enabled / choose one for this project / add one for every project (`--cluster`, `--db-server`) / allow or stop new work in one (staging starts disabled) / delete one nothing is left in |
| `goship clusters [-A]` | list the clusters the organization can use, its own and GoShip's shared one; `--all` spans every organization you belong to |
| `goship cluster create\|info\|sizes\|resize\|rm` | your own machines and Kubernetes clusters: `create` provisions a machine on a connected cloud (`--cloud --region --size`), connects one over SSH (`--host --ssh-key`) or connects a Kubernetes cluster (`--kubeconfig`); `--environment <slug>` (repeatable) points existing environments at the new cluster; `resize` changes the size of one GoShip created |
| `goship cloud providers\|connect\|list\|regions\|sizes\|disconnect` | the cloud accounts your clusters and database servers run on, shared by the whole organization — AWS connects with `--role-arn` or `--access-keys-stdin`; `connect <provider> --account <id>` replaces the key of an API-key account in place |
| `goship plans [--kind app\|database]` | what you can choose in the environment: priced on GoShip's cluster, sizes on your own |
| `goship releases` / `goship rollback` | deploy history |
| `goship orgs` / `goship org use <slug>` | list orgs / choose one |
| `goship projects` (or `project list`) / `goship project use\|create <slug>` | list the org's projects / choose one / create one |
| `goship tokens` / `goship token create\|rm` | list / manage API tokens for CI |
| `goship mcp` | serve all of the above to an AI agent over MCP (see below) |

Run `goship <command> --help` for flags.

## Upgrading to 0.16.0

- With no environment chosen, the CLI falls back to `development`, the new
  default environment.
- `goship environments` shows an `ENABLED` column, and `goship environment
  enable|disable <slug>` (MCP: `goship_enable_environment`,
  `goship_disable_environment`) turns an environment on or off.
- Staging is opt-in: enable it with `goship environment enable staging` before
  creating anything there.
- If your organization's apps moved to development, change or remove a saved
  `production` selection: `environment:` in `goship.yml`,
  `goship environment use`, or `GOSHIP_ENVIRONMENT`.

## Upgrading to 0.14.0

Apps, databases and volumes now live in an environment of a project, and your
own machines are managed as clusters that environments run on.

- `goship node …` and `goship nodes` are now `goship cluster …` and
  `goship clusters`. The old names print a pointer to the new ones on stderr
  and exit with status 2. `cluster create` takes `--cloud` (with `--region` and
  `--size`), `--host` (with `--ssh-key`) or `--kubeconfig`, plus an optional
  repeatable `--environment <slug>` that points existing environments at the
  new cluster.
- `--node` is gone from every command, and so is `node:` in `goship.yml` (it is
  ignored, with a warning). The environment decides where an app runs: choose
  it with `--environment`, `environment:` in `goship.yml`, `GOSHIP_ENVIRONMENT`
  or `goship environment use <slug>`; with none chosen it is the organization's
  default. To run an environment on one of your clusters, create it with
  `goship environment create <slug> --cluster <cluster>`.
- `goship plans --node` is gone; `goship plans --kind app|database` lists the
  plans of the environment (`db` is accepted for `database`).
- `goship cloud move` is gone: cloud accounts belong to the organization.
  `goship cloud connect <provider> --account <id>` reconnects an API-key
  account in place, keeping its clusters. `goship cloud` and `goship db server`
  take no project.
- `-A` on `apps`, `dbs` and `volumes` lists every environment of every project,
  and its `-o json` output is keyed by `org/project/environment` instead of
  `org/project`.
- MCP tools: the project tools take `environment`; the node and placement tools
  and `goship_move_cloud_account` are gone, replaced by
  `goship_list_environments`, `goship_create_environment`,
  `goship_delete_environment`, `goship_list_clusters`, `goship_get_cluster`,
  `goship_list_cluster_sizes`, `goship_create_cluster`,
  `goship_resize_cluster` and `goship_delete_cluster`. Update an installed
  Claude plugin to get the matching skill.

## Upgrading to 0.5.0

Organizations now hold projects, and apps, databases, nodes and volumes live in
a project.

- An organization with one project needs no change: the CLI picks it.
- With several, choose one with `goship project use <slug>`, `--project`,
  `GOSHIP_PROJECT` or `project:` in `goship.yml` (`goship init` writes it).
  `goship projects` lists them.
- `goship cloud connect gcp --project <id>` took the Google Cloud project id.
  `--project` now selects the GoShip project; the Google Cloud one is
  `--gcp-project`.
- `goship whoami` lists organizations, not teams.

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
`--output json` rather than pretend to support it. `cluster create` follows provisioning
progress instead (polls until the cluster is ready, for up to `--timeout`, 30 minutes by
default), unless `--no-wait` or `-o json` is passed. Ctrl-C stops following, not the
cluster: it keeps provisioning, and `goship cluster info <name>` shows where it got to.

`deploy` and `rollback` show their steps; when one fails, its whole output is
printed. `--verbose` (`-v`) shows the complete log as it arrives instead, and
works on every command (it also logs HTTP requests to stderr). Output that is
not a terminal gets one line per step, without colour or redraws.

## AI agents

`goship mcp` serves everything above as [MCP](https://modelcontextprotocol.io)
tools over stdio, using the login this machine already has. Claude Code users
get the server, a deploy skill and `/goship:deploy` in one install:

```
/plugin install goship --marketplace coffeece/goship
```

Any other MCP client runs `goship mcp` as a stdio server:

```json
{ "mcpServers": { "goship": { "command": "goship", "args": ["mcp"] } } }
```

Tools are named after the commands (`goship_list_apps`, `goship_deploy`,
`goship_logs`, `goship_create_database_server`…). Deletes are marked
destructive and ask the agent to confirm with you first; `login`,
`cloud connect` and `shell` stay in the terminal.

Every tool that acts on an app, database or volume takes `org`, `project` and
`environment`. Left out, the local server uses what it has selected —
`goship mcp --environment <slug>`, the `goship.yml` of its directory,
`GOSHIP_ENVIRONMENT` or `goship environment use` — and then the organization's
default; `goship_deploy` reads `environment:` from the directory it deploys
before any of those. Environments, clusters, cloud accounts and database servers
belong to the organization (`goship_list_environments`, `goship_list_clusters`,
`goship_create_cluster`…) and take `org` only. A tool that deletes something
should be given `environment` explicitly. `goship_create_cluster` takes SSH keys
and kubeconfigs as contents; connect those from the terminal instead
(`goship cluster create --host … --ssh-key <path>`, `--kubeconfig <path>`).

### Hosted MCP server (claude.ai connector)

For clients that cannot run a local process, GoShip hosts the same tools at:

```
https://mcp.goship.sh/mcp
```

It speaks Streamable HTTP and signs you in with your GoShip account over
OAuth, so there is no token to copy.

- **claude.ai** (also Claude Desktop and mobile): Customize > Connectors > Add
  custom connector, paste the URL, then sign in with GoShip when asked.
- **Claude Code**: `claude mcp add --transport http goship https://mcp.goship.sh/mcp`,
  then `/mcp` to sign in.

With no machine to read config from, there is no selected organization,
project or environment: tools use the only organization and project you have,
or ask for `org` and `project`, and the environment is the one you pass as
`environment`, else the organization's default.
Deploying a directory reads files on your machine, so `goship_deploy` only
works with the local `goship mcp`; everything else works the same.

The server is `cmd/goship-mcp`, deployed on GoShip itself; the `Procfile` at
the repository root is its. It holds no credential: it checks each request's token against
`https://goship.sh/.well-known/jwks.json` and calls the API with it.
`PORT`, `GOSHIP_API_URL`, `GOSHIP_MCP_URL` and `GOSHIP_ISSUER` override the
production defaults.

## Configuration

| | |
|---|---|
| `GOSHIP_API` | GoShip API base URL |
| `GOSHIP_ORG` | organization slug |
| `GOSHIP_PROJECT` | project slug |
| `GOSHIP_ENVIRONMENT` | environment slug |
| `GOSHIP_TOKEN` | bearer token |
| `GOSHIP_CONFIG` | config file path |

Defaults live in `~/.config/goship/config.json` (the API address, the token and
what `org use`, `project use` and `environment use` selected), written `0600`
because it holds a token. Flags beat environment variables, which beat the file. The
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
