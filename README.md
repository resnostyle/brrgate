# brrgate

Tiny HTTP shim for [autobrr external webhooks](https://autobrr.com/filters/external). autobrr `POST`s JSON macros to `/decide` and waits for HTTP `200` (continue the filter pipeline) versus any other status (`403`, etc.) to stop.

This repository targets **Go 1.22+** (pinned through [mise](https://mise.jdx.dev) and Docker). Use the mise-managed toolchain when your system Go is older.

## Features

- **Pattern authoring** via `patterns.yaml` using simple `substring` / `glob` (`path/filepath` rules) / `regex` entries evaluated top-to-bottom. `PATTERNS_FILE` wins over `AUTO_ACCEPT_REGEX`.
- **`DEBUG_APPROVAL`** mode blocks until you approve/deny in the UI while still showing what the matchers would do.
- **Per-request access logs** on both listeners: every HTTP request writes one `event=http_access` line (`server=webhook` or `server=debug`) with method, path, status, duration, and client metadata.
- **Detailed webhook logs** remain available at `LOG_LEVEL=debug` (JSON bodies plus per-rule evaluations for `POST /decide`).

### Tooling matrix

| Path | Purpose |
| --- | --- |
| [.mise.toml](.mise.toml) | Installs pinned Go (`1.22.12`), runs `vet`/`test`, shells out to Docker helpers. |
| [Dockerfile](Dockerfile) | Multi-stage build emitting a static-ish binary bundled into `alpine:3.20`. |
| [compose.yaml](compose.yaml) | Opinionated Compose stack wired for host-only debug UI publishes. |
| [.github/workflows/ci.yml](.github/workflows/ci.yml) | GitHub Actions: Go checks + Docker build; on **`main`**, pushes **`latest`** and **`0.0.<N>`** to GHCR where **`N`** is GitHub’s ever-increasing `run_number`. |

## HTTP routes

The binary runs **one HTTP server** on **`LISTEN_ADDR`** (default `:8080`). When **`DEBUG_APPROVAL=true`**, it also runs a **second server** on **`DEBUG_UI_LISTEN_ADDR`** (default `127.0.0.1:8091`) for the manual approval UI. Any other path returns **404**.

### Webhook server (`LISTEN_ADDR`)

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/decide` | Synchronous autobrr External filter hook. Send the JSON body configured in autobrr (macro-expanded). **`200`** ⇒ filter continues; **`403`** ⇒ filter stops (no regex/pattern match or debug deny / timeout). **`401`** ⇒ missing/invalid shared secret. **`400`** ⇒ body is not valid JSON. **`500`** ⇒ rare internal errors (for example entropy failure). Echoes **`X-Request-ID`** (generates one if the client did not send a safe `X-Request-ID`). |
| `GET` | `/healthz` | Liveness check; **`200`** with plain-text body `ok`. |

**Auth for `POST /decide`:** `Authorization: Bearer <WEBHOOK_SECRET>` **or** header `X-Webhook-Secret: <WEBHOOK_SECRET>`.

### Debug UI (`DEBUG_UI_LISTEN_ADDR`, only if `DEBUG_APPROVAL=true`)

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/` | HTML dashboard listing pending releases waiting for a decision. |
| `POST` | `/decision/{id}/approve` | Resolves the pending `{id}` in favor of **`200`** to autobrr. |
| `POST` | `/decision/{id}/deny` | Resolves as reject (**`403`** to autobrr). `{id}` is the decision token shown in logs and the dashboard. Redirects **`303`** back to `/`. |

The debug UI has **no built-in authentication**—bind it to loopback or put it behind a reverse proxy with auth/TLS.

Every HTTP request (including 404s) emits an **`event=http_access`** access line with `server=webhook` or `server=debug`, plus `method`, `path`, `status`, `duration_ms`, and `remote` (and optional `query`, `user_agent`, `request_id_in` when present).

## mise

```bash
mise trust   # first time checking out from git
mise install
mise exec -- go test ./...
```

Handy scripted tasks (`mise run <name>`):

- `ci` → `go vet` + `go test`
- `docker:build` → `docker build -t brrgate:local .`
- `docker:compose:{up|down}` → Compose helpers

Bump the pinned Go toolchain by editing `.mise.toml`, `Dockerfile`, and matching the `go` directive in [`go.mod`](go.mod).

## Run locally

```bash
export WEBHOOK_SECRET='replace-me'
export PATTERNS_FILE="$(pwd)/config/patterns.yaml" # ships in-repo; edit freely
mise exec -- go run ./...
```

Expose `http://HOST:LISTEN_ADDR/decide` to autobrr and set Expected HTTP Status to **`200`** for approvals.

Authenticate with either `Authorization: Bearer <secret>` **or** `X-Webhook-Secret`.

## Docker image

Requires [BuildKit](https://docs.docker.com/build/buildkit/) (Docker Desktop / Compose v2 enable it out of the box).

```bash
DOCKER_BUILDKIT=1 docker build -t brrgate:local .
docker run --rm \
  --env WEBHOOK_SECRET=changeme-long-random-token \
  --env PATTERNS_FILE=/config/patterns.yaml \
  -v "$(pwd)/config:/config:ro" \
  -p 8080:8080 \
  brrgate:local
```

Override `LISTEN_ADDR`, `MATCH_FIELD`, regex settings, logging, and debug knobs using the vars listed in [.env.example](.env.example).

Inside containers you generally want **`DEBUG_UI_LISTEN_ADDR=0.0.0.0:8091`** so host port-mapping can reach the approval UI—the Compose file already wires that unless you intentionally override `environment.DEBUG_UI_LISTEN_ADDR`.

### Compose shortcut

```bash
cp .env.example .env                     # set WEBHOOK_SECRET + matcher env (edit config/patterns.yaml rules)
DOCKER_BUILDKIT=1 docker compose up --build
```

- `compose.yaml` bind-mounts `${PATTERNS_DIR:-./config}` → `/config` (read-only). The process always reads **`/config/patterns.yaml`**, which ships from [`config/patterns.yaml`](config/patterns.yaml) in this repo—edit that file (or point `PATTERNS_DIR` at another directory that contains `patterns.yaml`). **Directory mounts avoid the Docker footgun** where a missing `./patterns.yaml` becomes a *directory* named `patterns.yaml`, producing `read ... is a directory`.
- Override published ports via `PUBLISH_LISTEN` / `PUBLISH_DEBUG`; the debug dashboard defaults to **`127.0.0.1` on the host** for safety (`DEBUG_APPROVAL=true` flows only).

Prefer `mise run docker:compose:up` if you mirror the scripted tasks locally.

**Troubleshooting:** if you previously bind-mounted a missing `./patterns.yaml`, Docker may have created a **directory** with that name. Remove it via `rm -rf ./patterns.yaml` (only when you are sure nothing important lives there) and rely on `config/patterns.yaml` + the directory mount above.

## GitHub Actions / GHCR

Workflow: [.github/workflows/ci.yml](.github/workflows/ci.yml).

| Trigger | What happens |
| --- | --- |
| Pull requests & pushes to any branch | `go vet`, `go test`, and `docker build` (tags `ghcr.io/<owner>/<repo>:ci-<sha>` locally in the runner—**not pushed**). |
| Push to **`main`** | Same checks, then **`docker push`** two tags to GitHub Container Registry: **`latest`** and **`0.0.<N>`**, where **`N`** is the workflow run counter GitHub exposes as `github.run_number`. |
| **Workflow dispatch** | Same rules as above based on the selected ref (typically rebuild `main`). |

Image reference shape: `ghcr.io/<github_repository_owner>/<repo_name>` (forced lowercase in the workflow).

**Repo settings:** enable **Actions → General → Workflow permissions → Read and write** (or grant **`packages: write`** via fine-grained tokens) so `GITHUB_TOKEN` can publish. Make the package public or grant pull access for downstream hosts.

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
