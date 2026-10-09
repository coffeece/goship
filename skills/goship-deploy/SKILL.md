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
- **Placement**: the shared cluster (billed per unit by plan) or one of the
  organization's own nodes (`goship_list_nodes`; apps there are not billed).
- **Plan**: `goship_list_plans` shows what the org can pick and the price.
  A plan with `billed: false` is free to choose. Never pick a paid plan
  without the user saying which.

## 2. Read the project

Before touching anything, determine and report:

1. **Platform**: the deploy infers it from the files present, in this order:
   `go.mod` → go, `pyproject.toml` / `requirements.txt` / `Pipfile` /
   `setup.py` → python, `package.json` → nodejs, `index.html` → static. A
   `Dockerfile` / `Containerfile` is the fallback when no marker matches, and
   is required for apps on the organization's own nodes.
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

A database, when needed:

- MCP: `goship_create_database` (plan from `goship_list_plans` with
  `kind: database`), then `goship_bind_database` to the app.
- CLI: `goship db create <app>-db --plan db-small` then
  `goship db bind <app>-db -a <app>`.

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

Destructive tools (`goship_delete_*`, `goship_revoke_api_token`) must be
confirmed with the user every time.
