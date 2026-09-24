# goship CLI — architecture

The CLI is a thin client for the GoShip API. It holds no platform logic of its
own: every command is one or a few API calls, and every call goes to the same
host (`https://api.goship.sh` unless `GOSHIP_API` says otherwise).

## Shape

```
cmd/goship/          wiring only: build the tree, map legacy names, print the error
internal/cli/        the cobra tree, declared explicitly — one file per noun
internal/portal/     client for the GoShip API: JSON calls, event streams, the shell
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
   refuse `--output json` instead of ignoring it.
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

## Org context

Every API route is org-scoped (`/orgs/{slug}/…`), so the CLI carries a current
org. It resolves, in order:

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

## Configuration

`~/.config/goship/config.json` (or `$GOSHIP_CONFIG`) holds the API address, the
selected org and the token, and is written `0600`. `GOSHIP_API`, `GOSHIP_ORG`
and `GOSHIP_TOKEN` override it for the process that sets them and are never
written back, so a CI token cannot end up on disk.

## Builds

The platform is inferred from marker files (`go.mod`, `pyproject.toml`,
`requirements.txt`, `Pipfile`, `setup.py`, `package.json`, `index.html`), in
that order. A `Dockerfile`, `dockerfile` or `Containerfile` is the fallback,
not a competitor: it decides the build only when no platform marker matches, so
a Go service that ships a Dockerfile is still built by the Go platform unless
`--dockerfile` says otherwise. An app built from a container file has no
platform at all; the image is its whole definition.

## Plans and placement

Creating an app needs a plan, and the CLI picks one only when it is free —
anything else spends the user's money, so it stops and prints the options with
prices. On a node the organization owns nothing is billed; the node's pool
decides which sizes are available and which applies by default, and the CLI
reports what the API says rather than working it out.

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
