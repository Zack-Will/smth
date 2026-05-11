---
name: smth-artifact-publish
description: Use this when a CLI agent should publish, update, inspect, or manage standalone HTML artifacts in SMTH. Trigger for visual reports, interactive previews, dashboards, matrices, mockups, or long structured outputs that benefit from HTML layout instead of Markdown.
---

# SMTH Artifact Publishing

> Markdown is boring, show me the HTML.

SMTH is a single-user HTML artifact shelf for CLI agents. It stores raw HTML
plus JSON metadata, then shows artifacts in a sidebar/iframe UI.

## When To Publish

Use SMTH when output benefits from visual layout, color, interactivity, or
spatial density: implementation plans with comparison tables, code review
reports with inline diff annotations, related-work matrices, eval dashboards,
design mockups, or anything longer than about 200 lines of equivalent Markdown.

Do not use SMTH for intermediate state another agent needs to parse, drafts
meant to be edited in an editor, files committed to a repo such as READMEs,
design docs, `CLAUDE.md`, short snippets, or anything normally git-tracked.

When in doubt, default to Markdown.

## Configuration

Use environment variables instead of hard-coding deployment details:

```sh
SMTH_URL="${SMTH_URL:-http://127.0.0.1:8080}"
SMTH_API_KEY="${SMTH_API_KEY:?set SMTH_API_KEY}"
```

Do not commit API keys. Do not put the write API key in iframe URLs. Browser
deployments should use `--public-read` behind a trusted LAN, Tailscale, Basic
Auth, or reverse-proxy access control.

## Publish A New Artifact

```sh
curl -fsS -X POST "$SMTH_URL/api/artifacts" \
  -H "X-API-Key: $SMTH_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"html":"<!doctype html><h1>Hello</h1>","title":"Demo","project":"demo","tags":["draft"]}'
```

For complex HTML, write JSON to a temp file and use `--data-binary @file` to
avoid shell quoting problems.

Required request field:
- `html`: raw standalone HTML. SMTH does not sanitize it.

Optional fields:
- `title`: preferred title. If omitted, server extracts `<title>` or first
  `<h1>`, then falls back to `untitled-HHMM`.
- `project`: grouping/filter label.
- `tags`: string array.
- `replace`: existing ULID to overwrite in place.

## Replace An Existing Artifact

```json
{
  "replace": "01KRB0KXB7NVJWB7BXDTDBHZJG",
  "html": "<!doctype html>...",
  "title": "Updated report",
  "project": "demo",
  "tags": ["final"]
}
```

SMTH emits an `update` SSE event. If the browser is viewing that id, the iframe
reloads via `srcdoc`.

## Inspect And Verify

List artifacts:

```sh
curl -fsS "$SMTH_URL/api/artifacts?limit=10" \
  -H "X-API-Key: $SMTH_API_KEY"
```

Read metadata:

```sh
curl -fsS "$SMTH_URL/api/artifacts/{id}" \
  -H "X-API-Key: $SMTH_API_KEY"
```

Read raw HTML:

```sh
curl -fsS "$SMTH_URL/a/{id}" \
  -H "X-API-Key: $SMTH_API_KEY"
```

Delete, soft-delete only:

```sh
curl -fsS -X DELETE "$SMTH_URL/api/artifacts/{id}" \
  -H "X-API-Key: $SMTH_API_KEY"
```

## Local Development

Run tests:

```sh
GOCACHE="$PWD/.cache/go-build" go test ./...
node --check static/app.js
```

Build:

```sh
GOCACHE="$PWD/.cache/go-build" go build -o smth-server ./cmd/smth-server
```

Run:

```sh
SMTH_API_KEY=change-me ./smth-server \
  --port 8080 \
  --data ./data \
  --static ./static \
  --max-size 2097152
```

Add `--public-read` only when read access is protected by network or reverse
proxy policy.

## Operational Notes

- The server stores `{ulid}.html` and `{ulid}.json` under `data/YYYY-MM-DD/`.
- SSE endpoint is `/api/stream`; event types are `new`, `update`, and `delete`.
- The frontend fetches raw HTML with headers and renders it with `iframe.srcdoc`
  so untrusted artifact code cannot read the API key from `window.location`.
- Raw HTML should be self-contained. `srcdoc` does not resolve relative asset
  paths the same way `/a/{id}` does.
- Default max HTML size is 2 MiB unless changed by `--max-size`.
