---
name: smth-artifact-publish
description: Use this when a CLI agent needs to publish, update, inspect, or deploy SMTH HTML artifacts. SMTH stores raw HTML files and metadata through a small Go server with filesystem storage and a sidebar/iframe UI.
---

# SMTH Artifact Publishing

SMTH is a single-user HTML artifact shelf. Use it when an agent has produced an
HTML report, preview, dashboard, plan, or other standalone artifact that should
be visible in the SMTH UI.

## Current Deployment

- Host alias: `home-nas-vm`
- Service: `systemctl --user status smth.service`
- Local service URL on the host: `http://127.0.0.1:18080`
- UI from the LAN: `http://home-nas-vm:18080/`
- Deploy dir: `/home/zack/apps/smth/current`
- Data dir: `/home/zack/apps/smth/shared/data`
- API key file: `/home/zack/apps/smth/shared/smth.env`

Do not hard-code the API key in repo files, commits, or final answers unless
the user explicitly asks for it. On the host, load it with:

```sh
. ~/apps/smth/shared/smth.env
```

## Publish A New Artifact

From `home-nas-vm`, POST JSON to `/api/artifacts`:

```sh
. ~/apps/smth/shared/smth.env
curl -fsS -X POST http://127.0.0.1:18080/api/artifacts \
  -H "X-API-Key: $SMTH_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"html":"<!doctype html><h1>Hello</h1>","title":"Demo","project":"demo","tags":["draft"]}'
```

For large or complex HTML, write JSON to a temp file and use `--data-binary
@file` to avoid shell quoting problems.

Required request field:
- `html`: raw standalone HTML. SMTH does not sanitize it.

Optional fields:
- `title`: preferred title. If omitted, server extracts `<title>` or first
  `<h1>`, then falls back to `untitled-HHMM`.
- `project`: grouping/filter label.
- `tags`: string array.
- `replace`: existing ULID to overwrite in place.

## Replace An Existing Artifact

Use `replace` to update the raw HTML and metadata for an existing artifact:

```json
{
  "replace": "01KRB0KXB7NVJWB7BXDTDBHZJG",
  "html": "<!doctype html>...",
  "title": "Updated report",
  "project": "demo",
  "tags": ["final"]
}
```

SMTH emits an `update` SSE event. If the browser is currently viewing that id,
the iframe reloads.

## Inspect And Verify

List artifacts:

```sh
. ~/apps/smth/shared/smth.env
curl -fsS "http://127.0.0.1:18080/api/artifacts?limit=10" \
  -H "X-API-Key: $SMTH_API_KEY"
```

Read metadata:

```sh
curl -fsS http://127.0.0.1:18080/api/artifacts/{id} \
  -H "X-API-Key: $SMTH_API_KEY"
```

Read raw HTML:

```sh
curl -fsS "http://127.0.0.1:18080/a/{id}?api_key=$SMTH_API_KEY"
```

Check logs:

```sh
journalctl --user -u smth.service -n 50 --no-pager
```

Logs redact `api_key` query values. Avoid printing `$SMTH_API_KEY` in logs or
user-facing output unless explicitly requested.

## Delete

Deletion is soft delete. It adds `deleted_at` to metadata and removes the item
from normal lists:

```sh
. ~/apps/smth/shared/smth.env
curl -fsS -X DELETE http://127.0.0.1:18080/api/artifacts/{id} \
  -H "X-API-Key: $SMTH_API_KEY"
```

## Local Development

Run tests:

```sh
GOCACHE="$PWD/.cache/go-build" go test ./...
node --check static/app.js
```

Build locally:

```sh
GOCACHE="$PWD/.cache/go-build" go build -o smth-server ./cmd/smth-server
```

Run locally:

```sh
SMTH_API_KEY=change-me ./smth-server \
  --port 8080 \
  --data ./data \
  --static ./static \
  --max-size 2097152
```

Use `--public-read` only when a reverse proxy, LAN boundary, or other access
control protects read access.

## Deploy To home-nas-vm

Build Linux amd64 and ship `smth-server + static/` to a release directory:

```sh
GOOS=linux GOARCH=amd64 GOCACHE="$PWD/.cache/go-build" \
  go build -o /tmp/smth-server-linux-amd64 ./cmd/smth-server
```

Expected service shape:

```ini
[Service]
EnvironmentFile=%h/apps/smth/shared/smth.env
WorkingDirectory=%h/apps/smth/current
ExecStart=%h/apps/smth/current/smth-server --port 18080 --data %h/apps/smth/shared/data --static %h/apps/smth/current/static --max-size 2097152
Restart=always
```

After deploying:

```sh
ssh home-nas-vm 'systemctl --user restart smth.service'
ssh home-nas-vm 'systemctl --user is-active smth.service'
ssh home-nas-vm 'curl -fsS -I http://127.0.0.1:18080/ | sed -n "1p"'
```

## Operational Notes

- `8080` was already occupied on `home-nas-vm`; SMTH uses `18080`.
- The server stores raw HTML as `{ulid}.html` and metadata as `{ulid}.json`
  under `data/YYYY-MM-DD/`.
- SSE endpoint is `/api/stream`; event types are `new`, `update`, and `delete`.
- The frontend iframe uses `/a/{id}` for raw HTML and does not navigate the
  whole page on sidebar selection.
- Default max HTML size is 2 MiB unless changed by `--max-size`.
