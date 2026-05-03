# autobrr-hook

Tiny HTTP shim for [autobrr external webhooks](https://autobrr.com/filters/external). autobrr `POST`s JSON macros to `/decide` and waits for HTTP `200` (continue the filter pipeline) versus any other status (`403`, etc.) to stop.

This repository targets **Go 1.22+** (pinned through [mise](https://mise.jdx.dev) and Docker). Use the mise-managed toolchain when your system Go is older.

## Features

- **Pattern authoring** via `patterns.yaml` using simple `substring` / `glob` (`path/filepath` rules) / `regex` entries evaluated top-to-bottom. `PATTERNS_FILE` wins over `AUTO_ACCEPT_REGEX`.
- **`DEBUG_APPROVAL`** mode blocks until you approve/deny in the UI while still showing what the matchers would do.
- **Structured plain-text logs** (`LOG_LEVEL=debug` dumps bodies plus per-rule evaluation rows).

### Tooling matrix

| Path | Purpose |
| --- | --- |
| [.mise.toml](.mise.toml) | Installs pinned Go (`1.22.12`), runs `vet`/`test`, shells out to Docker helpers. |
| [Dockerfile](Dockerfile) | Multi-stage build emitting a static-ish binary bundled into `alpine:3.20`. |
| [compose.yaml](compose.yaml) | Opinionated Compose stack wired for host-only debug UI publishes. |

## mise

```bash
mise trust   # first time checking out from git
mise install
mise exec -- go test ./...
```

Handy scripted tasks (`mise run <name>`):

- `ci` → `go vet` + `go test`
- `docker:build` → `docker build -t autobrr-hook:local .`
- `docker:compose:{up|down}` → Compose helpers

Bump the pinned Go toolchain by editing `.mise.toml`, `Dockerfile`, and matching the `go` directive in [`go.mod`](go.mod).

## Run locally

```bash
export WEBHOOK_SECRET='replace-me'
export PATTERNS_FILE="$(pwd)/patterns.yaml" # copy patterns.example.yaml
mise exec -- go run ./...
```

Expose `http://HOST:LISTEN_ADDR/decide` to autobrr and set Expected HTTP Status to **`200`** for approvals.

Authenticate with either `Authorization: Bearer <secret>` **or** `X-Webhook-Secret`.

## Docker image

Requires [BuildKit](https://docs.docker.com/build/buildkit/) (Docker Desktop / Compose v2 enable it out of the box).

```bash
DOCKER_BUILDKIT=1 docker build -t autobrr-hook:local .
docker run --rm \
  --env WEBHOOK_SECRET=changeme-long-random-token \
  --env PATTERNS_FILE=/config/patterns.yaml \
  -v "$(pwd)/patterns.yaml:/config/patterns.yaml:ro" \
  -p 8080:8080 \
  autobrr-hook:local
```

Override `LISTEN_ADDR`, `MATCH_FIELD`, regex settings, logging, and debug knobs using the vars listed in [.env.example](.env.example).

Inside containers you generally want **`DEBUG_UI_LISTEN_ADDR=0.0.0.0:8091`** so host port-mapping can reach the approval UI—the Compose file already wires that unless you intentionally override `environment.DEBUG_UI_LISTEN_ADDR`.

### Compose shortcut

```bash
cp patterns.example.yaml patterns.yaml   # customise rules as needed
cp .env.example .env                     # set WEBHOOK_SECRET + matcher env
DOCKER_BUILDKIT=1 docker compose up --build
```

- `compose.yaml` mounts `${PATTERNS_PATH:-./patterns.yaml}` onto `/config/patterns.yaml`; override `PATTERNS_PATH` when your rules live elsewhere.
- Override published ports via `PUBLISH_LISTEN` / `PUBLISH_DEBUG`; the debug dashboard defaults to **`127.0.0.1` on the host** for safety (`DEBUG_APPROVAL=true` flows only).

Prefer `mise run docker:compose:up` if you mirror the scripted tasks locally.

## autobrr external JSON example

```json
{
  "torrent": "{{ .TorrentName }}",
  "indexer": "{{ .Indexer }}",
  "size_bytes": "{{ .TorrentSize }}"
}
```

Ensure the `MATCH_FIELD` string matches whichever top-level JSON key carries the searchable text (`torrent` in the snippet above).

## Safety notes

- Do **not** log secrets; only success/failure of auth is recorded.
- Generate a long random `WEBHOOK_SECRET`.
- Manual approvals deliberately block autobrr—tune `DEBUG_TIMEOUT` appropriately.
- `DEBUG_APPROVAL=true` should run behind loopback tunnels or authenticated proxies whenever possible.
