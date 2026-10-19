---
name: goship-deploy
description: Deploy or port a project to GoShip (goship.sh). Use when the user wants to deploy, ship, host, publish, migrate or onboard an app, API, site or bot on GoShip, or asks for a URL for something they built. Triggers include "deploy to goship", "goship deploy", "put this online", "give it a database", "ship this".
version: 1.0.0
---

# Deploy to GoShip

You are taking a project from a directory to a running URL on GoShip. Work
through the phases in order and report what you found before changing
anything.

Use the `goship_*` MCP tools when they are available; otherwise run the
`goship` CLI with the same arguments (`references/cli.md` maps one to the
other). Both need a login: `goship_whoami` or `goship whoami` must succeed. If
not, ask the user to run `goship login` in a terminal (it opens a browser) and
wait; never ask for a password.

## 1. Where it goes

Find out, asking only for what is not already known:

- **Directory** to deploy (default: the current one).
- **App name**: lowercase letters, digits, hyphens. Default: the directory name.
- **Organization**: `goship_list_orgs` shows them; one org needs no choice.
- **Project**: `goship_list_projects` shows the organization's; one project
  needs no choice. Pass `project` to the tools, or set `project:` in goship.yml.
- **Placement**: the shared cluster (billed per unit by plan) or one of the
  project's own nodes (`goship_list_nodes`; apps there are not billed).
- **Plan**: `goship_list_plans` shows what the project can pick and the price.
  A plan with `billed: false` is free to choose. Never pick a paid plan
  without the user saying which.

## 2. Read the project

Before touching anything, determine and report:

1. **Platform**: the deploy infers it from the files present, in this order:
   `go.mod` → go, `pyproject.toml` / `requirements.txt` / `Pipfile` /
   `setup.py` → python, `package.json` → nodejs, `index.html` → static. A
   `Dockerfile` / `Containerfile` is the fallback when no marker matches, and
   is required for apps on the project's own nodes.
2. **Entry point** and how it is started (a Procfile, `processes:` in
   `goship.yaml`, or the platform default).
3. **Port**: the app must listen on `0.0.0.0:8888`, or read `$PORT`.
4. **Configuration**: what it reads from the environment, which values are
   secrets.
5. **Database**: whether it needs PostgreSQL.
6. **Health endpoint**: an existing `/healthz` or `/health`; the release is
   only promoted once it answers 200.
7. **Build steps**: code generation, asset builds → `hooks.build`.
8. **Secrets on disk**: `.env` and the like, which must not be uploaded.
9. **Existing deploy files**: `Procfile`, `goship.yaml`, `.goshipignore`.

## 3. Changes the app needs

Keep them minimal and only what GoShip needs:

- Port from `$PORT`, defaulting to 8888, bound to `0.0.0.0`.
- Configuration from environment variables.
- A `/healthz` route returning 200 if there is none.
- Graceful shutdown on SIGTERM.
- JSON or plain logs to stdout; no log files.

Multi-port apps consolidate onto one port or become separate apps; discuss
the trade-off with the user.

## 4. Deployment files

`goship.yaml` carries both what the CLI needs and what the platform reads.
`references/goship-yaml.md` has the full key list. A minimal one:

```yaml
app: api
platform: go          # or: dockerfile: Dockerfile
plan: app-small
env:
  LOG_LEVEL: info     # applied as private on every deploy; secrets go elsewhere
healthcheck:
  path: /healthz
```

- Go: the platform runs `go install ./...` and puts binaries in
  `/home/application/bin/`, so a Procfile reads `web: /home/application/bin/<name>`.
- Python / Node: `web: <start command>`.
- Write a `.goshipignore` (gitignore syntax) with at least `.env`,
  `node_modules/`, `*.log`. Without one everything in the directory is
  uploaded, up to 100 MB.
- A Dockerfile build must `COPY goship.yaml ./` into its `WORKDIR`.

## 5. Deploy

One call creates the app on first deploy, applies env and follows the build:

- MCP: `goship_deploy` with `path`, and `app` / `plan` / `node` / `env` /
  `dockerfile` when they differ from the file.
- CLI: `goship deploy [dir] [--plan p] [--node n] [--env-file .env] [-m msg]`.

Secrets go in `env` (MCP) or `--env-file` (CLI); both apply them as private
variables. Never write secrets into `goship.yaml`.

A database, when needed. `goship_list_plans` with `kind: database` lists
the plans: each sets a storage and connection quota and a monthly price,
and every plan is billed on GoShip's server, so name the plan and its price
to the user before creating one.

- MCP: `goship_create_database` with `name` and `plan`, then
  `goship_bind_database` to the app.
- CLI: `goship db create <app>-db --plan db-micro` then
  `goship db bind <app>-db -a <app>`.

**In the customer's own cloud, unbilled.** When the project or its
organization has a Google Cloud account connected
(`goship_list_cloud_accounts`, provider `gcp`; `goship cloud connect gcp`
connects one in a terminal), the database can live on a *database server*:
a Cloud SQL instance GoShip creates in that account, paid to Google by the
customer, on which GoShip bills nothing. Only Google Cloud hosts database
servers, one per cloud account.

1. `goship_list_database_servers`: a server with status `ready` is reused.
   Otherwise `goship_create_database_server` with `cloud_account` (id, label
   or `gcp`) and `region` (`goship_list_cloud_regions`); `tier` comes from
   `goship_list_database_server_tiers`, default `db-f1-micro`; `name`
   defaults to `pg-<account label>`. Provisioning runs for about ten
   minutes in the background; do not wait for it.
2. `goship_create_database` with `server` set to the server's name. The
   plan still sets the quotas but is not billed. While the server is still
   provisioning the database is queued (status `pending`) and created once
   the server is ready; `goship_get_database` shows `status` and `detail`.
3. `goship_bind_database` once the database is `ready`.

CLI: `goship db server create --account gcp --region southamerica-east1`,
then `goship db create <app>-db --plan db-small --server pg-gcp`.

The instance has a public IP and accepts TLS only, so apps on GoShip's
cluster and on the project's own nodes reach it alike; `DATABASE_URL`
carries `sslmode=require`. A server that fails to come up carries
`error_message`: an account connected before October 2026 must run
`goship cloud connect gcp` again, the Google project needs an active billing
account, and an organization policy blocking public IP on Cloud SQL must be
disabled by the customer. Fix the cause, then `goship_retry_database_server`.

Binding injects `DATABASE_URL`, `DATABASE_HOST/PORT/NAME/USER/PASSWORD`,
`DATABASE_SSLMODE` and `PGSSLMODE`, and restarts the app.

The deploy returns the version and the public URL. If the connection drops,
the release keeps running on GoShip: `goship_list_releases` shows how it
ended. On failure, the result carries the last lines of the build log; fix
the cause and deploy again rather than retrying blindly.

## 6. Verify

1. `goship_get_app`: status `running`, units healthy, the address.
2. Request the URL (or its health route) and confirm a 200.
3. `goship_logs` for the first lines after start; look for crashes.

Report the URL, what was created (app, database, plan and its price) and
anything the user still has to do (DNS for a custom domain, secrets to
rotate).

## Afterwards

- Env changes: `goship_set_env` / `goship env set` (restarts unless
  `no_restart`).
- More units: `goship_scale_app`.
- Custom domain: `goship_register_domain` → publish the TXT → `goship_verify_domain`
  → `goship_add_app_domain`, then CNAME the hostname to the app's address.
- Roll back: `goship_rollback` with a version from `goship_list_releases`.
- One-off commands (migrations): `goship_run`.
- PostgreSQL extensions (PostGIS, pg_trgm, citext, pgcrypto, uuid-ossp…):
  `goship_list_database_extensions` shows enabled and available ones,
  `goship_enable_database_extension` turns one on (status `enabling` until the
  server finishes; list again), `goship_disable_database_extension` turns it
  off while no table uses it. `vector` (pgvector) is always on. Enable them
  before running migrations that depend on them.
- Cloud account scope: `goship_move_cloud_account` moves an account to one
  project or shares it with the whole organization.
- Removing a database server: delete its databases first
  (`goship_delete_database` drops the data on any server), then
  `goship_delete_database_server` removes the Cloud SQL instance from the
  customer's project. `force` only forgets the server in GoShip, for an
  instance GoShip can no longer reach; it keeps running and billing at Google
  until the customer deletes it there.

Destructive tools (`goship_delete_*`, `goship_revoke_api_token`) must be
confirmed with the user every time.
