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

<details>
<summary>Ask an agent to deploy SMTH</summary>

Copy this prompt to a CLI agent that has SSH access to your target Linux host:

```text
Deploy this SMTH repo to my Linux server.

Inputs:
- SSH target: <user@host or ssh alias>
- Public/LAN URL I will use in the browser: <http(s)://...>
- Port to listen on: <port, default 8080>
- Install root: <remote path, default ~/apps/smth>
- Read access boundary: <LAN / Tailscale / reverse proxy auth / other>

Requirements:
1. Inspect the remote host first: OS, architecture, available ports, systemd
   user support, existing SMTH install, and whether the requested port is free.
2. Build the correct Linux binary from this repo. For x86_64 use:
   `GOOS=linux GOARCH=amd64 go build -o smth-server ./cmd/smth-server`.
   For ARM64 use `GOOS=linux GOARCH=arm64`.
3. Deploy as:
   `<install-root>/releases/<git-sha>/smth-server`
   `<install-root>/releases/<git-sha>/static/`
   `<install-root>/shared/data/`
   `<install-root>/shared/smth.env`
   `<install-root>/current -> releases/<git-sha>`
4. Generate `SMTH_API_KEY` on the server if it does not exist. Store it only in
   `<install-root>/shared/smth.env` with mode 600. Do not commit it or print it
   unless I explicitly ask for it.
5. Create a user-level systemd service if possible:
   `smth-server --port <port> --data <install-root>/shared/data --static <install-root>/current/static --max-size 2097152 --public-read`
   Use `--public-read` only because the browser UI needs read access for SSE and
   artifact preview; protect reads with my stated LAN/Tailscale/reverse-proxy
   boundary. Write endpoints must still require `X-API-Key`.
6. Enable and start the service, then verify:
   - service is active
   - `GET /` returns 200
   - `GET /api/artifacts?limit=1` works from the protected read boundary
   - unauthenticated `POST /api/artifacts` returns 401
   - authenticated `POST /api/artifacts` can create a small smoke artifact
   - raw HTML can be read back
   - logs do not leak `SMTH_API_KEY`
7. Leave me with the URL, service name, install paths, key file path, and the
   exact commands for status, logs, restart, and rollback.

Do not expose the write API key in iframe URLs or query strings. If you change
the repo while deploying, run `go test ./...` and `node --check static/app.js`.
```

</details>

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
access control; do not put the write API key into iframe URLs. The browser UI
is intentionally read-only. Create, replace, and delete artifacts from CLI
agents or scripts with the write API key.

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
