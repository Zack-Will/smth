# SMTH

> Markdown is boring, show me the HTML.

A single-user HTML artifact shelf for CLI agents. SMTH stores each artifact as
raw HTML plus JSON metadata on the filesystem, and serves a small sidebar/canvas
UI for previewing artifacts in a sandboxed iframe.

## Build

```sh
go build -o smth-server ./cmd/smth-server
```

## Run

```sh
SMTH_API_KEY=change-me ./smth-server --port 8080 --data ./data --public-read --max-size 2097152
```

## Project Layout

```text
.
├── cmd/smth-server/      # Go single-binary server and tests
├── data/                 # Runtime artifact store
├── design/               # Original design handoff archive
├── static/               # Frontend files served by the binary
├── go.mod
└── README.md
```

Flags:

- `--port 8080`: HTTP port.
- `--data ./data`: filesystem storage root.
- `--static ./static`: frontend directory.
- `--public-read`: allow unauthenticated `GET` and SSE reads.
- `--max-size 2097152`: max raw HTML size in bytes.
- `--base-url https://smth.example.com`: optional public URL for create responses.

All write endpoints require `X-API-Key: $SMTH_API_KEY`. Read endpoints also
require auth unless `--public-read` is set. For browser use, prefer
`--public-read` behind a trusted LAN, Tailscale, Basic Auth, or reverse-proxy
access control; do not put the write API key into iframe URLs.

## Storage

```text
data/
  2026-05-10/
    01HXYZ...abc.html
    01HXYZ...abc.json
```

Metadata:

```json
{
  "id": "01HXYZ...",
  "title": "live migration plan v2",
  "project": "migration-paper",
  "tags": ["plan", "draft"],
  "created_at": "2026-05-10T14:23:00Z",
  "updated_at": "2026-05-10T14:23:00Z",
  "size_bytes": 12345
}
```

## API

### Create or Replace

`POST /api/artifacts`

```json
{
  "html": "<!doctype html>...",
  "title": "live migration plan v2",
  "project": "migration-paper",
  "tags": ["plan", "draft"],
  "replace": "01HXYZ..."
}
```

`replace` is optional. Without it, SMTH creates a new ULID artifact. With it,
SMTH overwrites the existing HTML and updates metadata in place.

### List

`GET /api/artifacts?project=&limit=50&before={ulid}`

Returns newest-first metadata. Deleted artifacts are filtered out.

### Metadata

`GET /api/artifacts/{id}`

### Raw HTML

`GET /a/{id}`

Returns the original HTML as `text/html`. This endpoint is intended for iframe
preview.

### Delete

`DELETE /api/artifacts/{id}`

Soft-deletes the artifact by adding `deleted_at` to metadata.

### Stream

`GET /api/stream`

Server-Sent Events:

- `new`: `{id, title, project, created_at}`
- `update`: `{id, updated_at}`
- `delete`: `{id}`

The server emits `: heartbeat` every 30 seconds and replays the latest 100
events after `Last-Event-ID` reconnects.

---

Project name riffs on Linus Torvalds' [LKML reply from 2000-08-25](https://lkml.org/lkml/2000/8/25/132):
"Talk is cheap. Show me the code."
