# goship.yaml

Also accepted: `goship.yml`, `.goship.yaml`, `.goship.yml`. `goship init`
writes the CLI half from what it finds in the directory.

## CLI keys

| Key | Meaning |
|---|---|
| `app` | app name (default: directory name) |
| `org` | organization slug, instead of `goship org use` |
| `platform` | `go`, `python`, `nodejs`, `static` |
| `dockerfile` | build from this container file instead of a platform |
| `plan` | plan slug for a new app |
| `node` | run on this node of the organization's own (never billed) |
| `env` | variables applied as private on every deploy |

## Platform keys

```yaml
healthcheck:
  path: /healthz
  scheme: http
  allowed_failures: 3
  interval_seconds: 10
  timeout_seconds: 5
  deploy_timeout_seconds: 120
  force_restart: true
  # or: command: ["/bin/sh", "-c", "..."]
startupcheck:
  path: /healthz          # same fields minus deploy_timeout_seconds/force_restart
hooks:
  build: [npm run build]
  restart:
    before: [./scripts/warmup.sh]
    after: [python manage.py migrate]
processes:                # overrides the Procfile
  - name: web
    command: gunicorn app:app --bind 0.0.0.0:$PORT
    healthcheck: {path: /ready}
  - name: worker
    command: python worker.py
```

A Dockerfile build reads the file from the image's `WORKDIR`:
`COPY goship.yaml ./`.

## Platforms

| Platform | Detected from | Build |
|---|---|---|
| go | `go.mod` | `go install ./...`; binaries in `/home/application/bin/` |
| python | `pyproject.toml`, `requirements.txt`, `Pipfile`, `setup.py` | pip install; Python 3.14 — prefer loose `>=` pins for native deps |
| nodejs | `package.json` | npm install |
| static | `index.html` | nginx serves the directory |
| Dockerfile | `Dockerfile` / `Containerfile`, when no marker matches | image build; required on the organization's own nodes |

The port is always 8888 (`$PORT`).

## Upload

Ignore file: the first of `.goshipignore`, `.dockerignore`
(gitignore syntax). `.git` is always excluded. Limit 100 MB.

## Addresses

Shared cluster: `https://<app>-<token>.apps.goship.sh`. Own nodes:
`https://<app>.<node-token>.apps.goship.sh`. Read the real one from the
deploy result or `goship_get_app`. A custom domain is a CNAME to it; TLS is
automatic.
