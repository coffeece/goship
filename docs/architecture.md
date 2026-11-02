# goship CLI — architecture

The CLI is a thin client for the GoShip API. It holds no platform logic of its
own: every command is one or a few API calls, and every call goes to the same
host (`https://api.goship.sh` unless `GOSHIP_API` says otherwise).

## Shape

```
cmd/goship/          wiring only: build the tree, map legacy names, print the error
internal/cli/        the cobra tree, declared explicitly — one file per noun
internal/portal/     client for the GoShip API: JSON calls, event streams, the shell
internal/deploy/     the decisions a deploy makes (target, platform, plan), shared by the CLI and the MCP tools
internal/mcptools/   the MCP tools, one per command; internal/mcphost serves them over HTTP
internal/render/     table | json — the only place command results are formatted
internal/config/     the config file, and the GOSHIP_* variables that override it
internal/archive/    the project directory as the gzipped tarball a deploy uploads
internal/oauthlogin/ browser sign-in: authorization code + PKCE on a loopback port
```

### Three rules

1. **Commands don't format.** A data command hands a value to `render`, which
   decides table or JSON. That is what makes `--output json` work everywhere
   rather than command by command. Table columns are the struct fields tagged
   `table:"HEADER"`.
2. **Streaming commands say so.** `deploy`, `logs`, `run`, `shell` and
   `rollback` write output as it arrives, so there is nothing to format; they
   refuse `--output json` instead of ignoring it. `cluster create` polls for
   provisioning progress instead of streaming.
3. **The tree is written down.** Every subcommand is attached explicitly in
   `internal/cli`, so a dead branch is a test failure, not a surprise in help.

## Talking to the API

Ordinary calls are JSON over HTTPS with a bearer token, and each request says
which CLI version sent it (`X-Goship-Version`), so the API can tell an
outdated binary to upgrade.

Long operations — a deploy, a rollback, a one-off command, a followed log —
answer with an NDJSON event stream: `output` lines, `step` changes, keep-alive
`ping`s and one final `result`. A stream that ends without a result is a
dropped connection, reported as such; a release keeps running on the server
either way. A deploy uploads the archive in the same request as it is packed,
so a project is never held in memory or written to disk first.

`goship shell` is a WebSocket to the same API.

Cloud credential connections (OAuth, like `goship cloud connect digitalocean`) open the
browser on the GoShip dashboard's connect page
(`/orgs/{slug}/settings/cloud-accounts/connect/{ticket}`), which hands off to the
provider; that browser has to be signed in to GoShip. The CLI polls
`GET /cloud-accounts/connect/{ticket}` every 2 seconds for up to 10 minutes, waiting
for the OAuth flow to finish in the browser and reach the portal API. `cluster create`
follows provisioning the same way, bounded by `--timeout` (30 minutes by default).
Both polls ride out up to three transient failures in a row (a network error or a
5xx) and stop at once on a 4xx; Ctrl-C stops the wait, not the operation.

## Org, project and environment context

Account and org-level routes live under `/orgs/{slug}/…`: environments,
clusters, cloud accounts, database servers and registered domains. What a
project holds lives under `/orgs/{slug}/projects/{project}/…`, and apps,
databases, volumes and plans one level further, in one environment of that
project: `/orgs/{slug}/projects/{project}/environments/{env}/…`. The CLI
therefore carries a current org, project and environment. The org resolves, in
order:

1. `--org`
2. `org:` in the working directory's `goship.yml` — a repo belongs to one
   organization, which is a more specific statement than a global selection
3. `GOSHIP_ORG`, then what `goship org use` persisted
4. the only organization the account belongs to, when there is just one
5. for `deploy` and `app info`, the organization that already has an app of
   that name

Commands that change something fail with a pointer to `org use` only when all
of those come up empty. Listings (`apps`, `dbs`, …) with no org selected show
every org instead.

The project inside that org resolves the same way: `--project`, `project:` in
`goship.yml` (ignored when the file pins another org), `GOSHIP_PROJECT`, what
`goship project use` persisted for the org, then the only project the user can
reach. With several and none selected, a command that changes something fails
with a pointer to `project use`; `deploy` and `app info` look for the app in
every project and environment instead, and listings show every project and
environment as `org/project/environment`.

The environment resolves after the project, in order:

1. `--environment`
2. `environment:` in the working directory's `goship.yml`, only when the file's
   `org` and `project` are empty or the same as the target's: a file speaks for
   the project it names, as its `project:` does for its org
3. `GOSHIP_ENVIRONMENT`
4. what `goship environment use` persisted for the project
5. the organization's default environment, read from `GET /orgs`, which is only
   asked when 1–4 chose nothing

When 1–4 chose nothing and the default cannot be read, the answer is
`development` — but only for commands that read: they say so once on stderr. A
command that creates or changes something refuses and asks for `--environment`,
rather than act in a guessed environment. `deploy` finds the app by name when
no project is selected, in the environment that holds it; an app of the same
name in another project is reported with the flags and `use` commands that
select it. The same name in another environment of the same project is
another app, and a deploy creates it there.

`goship init` writes `environment:` into `goship.yml` only when the selected
environment is not the organization's default, since every deploy lands in the
default anyway. A `node:` key in `goship.yml` is ignored, with a warning once
per file.

Cloud accounts, database servers and clusters belong to the organization, and
so do the environments themselves: `cloud`, `db server`, `cluster`, `clusters`,
`environments` and `environment create|rm` take no project and use the
org-level routes. Only `environment use` needs one, since the choice it saves
is per project.

The MCP tools resolve the same way, per call: the tool's `org`, `project` and
`environment` arguments, then what the server process has selected (the local
`goship mcp` reads the same flag, file, variable and saved choice as a command;
the hosted server has none), then the organization's default. `goship_deploy`
puts the deployed directory's `goship.yml` between its `environment` argument
and the server's selection. A failure to read the default is an error for every
tool, reads included, since a tool call has no stderr to note a guess on.

## Configuration

`~/.config/goship/config.json` (or `$GOSHIP_CONFIG`) holds the API address, the
selected org, the selected project per org, the selected environment per
`org/project` and the token, and is written `0600`. `GOSHIP_API`, `GOSHIP_ORG`,
`GOSHIP_PROJECT`, `GOSHIP_ENVIRONMENT` and `GOSHIP_TOKEN` override it for the
process that sets them and are never written back, so a CI token cannot end up
on disk.

## Builds

The platform is inferred from marker files (`go.mod`, `pyproject.toml`,
`requirements.txt`, `Pipfile`, `setup.py`, `package.json`, `index.html`), in
that order. A `Dockerfile`, `dockerfile` or `Containerfile` is the fallback,
not a competitor: it decides the build only when no platform marker matches, so
a Go service that ships a Dockerfile is still built by the Go platform unless
`--dockerfile` says otherwise. An app built from a container file has no
platform at all; the image is its whole definition.

## Plans and clusters

Creating an app needs a plan, and the CLI picks one only when it is free —
anything else spends the user's money, so it stops and prints the options with
prices. The plans on offer are those of the environment's cluster. On a cluster
the organization owns nothing is billed by plan; the cluster decides which
sizes are available and which applies by default, and the CLI reports what the
API says rather than working it out. Which cluster an environment runs on is
set when the environment is created (`environment create --cluster`); there is
no per-app placement.

## Authentication

`goship login` opens the browser on the API's authorization endpoint (RFC 8252
with PKCE), receives the code on a loopback port and exchanges it. The token
that comes back lasts hours, so the CLI immediately trades it for a 90-day API
token named after the machine: it appears in the dashboard, and
`goship logout` revokes it. `--email` signs in with a password where there is
no browser.

For CI, `goship token create` mints a token to put in `GOSHIP_TOKEN`.

## Compatibility

Command names from the CLI's first release (`app-list`, `env-set`,
`volume-create`, …) are rewritten to their replacements before the tree sees
them, so old scripts keep working without cluttering help. `<noun> list` stays
as a deprecated spelling of the plural lister.
