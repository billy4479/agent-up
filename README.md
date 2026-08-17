# agent-up

`agent-up` lets agents upload files and directories to a small HTTP service so users can open them in a browser. It has no authentication and is intended to run only behind a VPN or another trusted network boundary.

## Server

Run the backend with:

```sh
agent-up-server
```

Configuration:

| Variable | Default | Description |
| --- | --- | --- |
| `AGENTUP_LISTEN` | `:8080` | HTTP listen address |
| `AGENTUP_DATA_DIR` | `./data` | Persistent upload directory |
| `AGENTUP_MAX_UPLOAD_SIZE` | unlimited | Maximum request body size in bytes; must be positive when set |

For example:

```sh
AGENTUP_LISTEN=127.0.0.1:8080 \
AGENTUP_DATA_DIR=/var/lib/agent-up \
AGENTUP_MAX_UPLOAD_SIZE=104857600 \
agent-up-server
```

Each upload is written to a temporary directory under `AGENTUP_DATA_DIR` and atomically renamed to its public slug after the body and manifest are complete. Failed and oversized uploads are removed. Keep the data directory on persistent storage and do not edit its internal layout while the server is running.

## CLI

Set `AGENTUP_URL` to the externally reachable backend URL:

```sh
export AGENTUP_URL=https://agent-up.example.internal
```

Upload files, HTML sites, stdin, or directories:

```sh
agent-up ./report.pdf
agent-up ./index.html
agent-up ./weird-file --mime plain/text
agent-up - --name image.jpg --mime image/jpeg < image.jpg
agent-up ./site-directory
```

Options may appear before or after the path. `--name` is required for stdin and is otherwise rejected. `--mime` sets the exact served `Content-Type`; without it, the server infers the type from the filename extension and content. The CLI prints only the resulting public URL to stdout.

Directory uploads are streamed as uncompressed tar archives. Symlinks and non-regular filesystem entries are rejected. The server also rejects archive traversal paths, duplicate paths, links, and special files.

## Public URLs

- Ordinary files are served at `/<slug>/<filename>`.
- A single `index.html` is served at `/<slug>`.
- Directory roots redirect from `/<slug>` to `/<slug>/` so relative links work.
- A directory's `index.html` is rendered when present; otherwise the server generates an escaped navigable listing.

All responses include `X-Content-Type-Options: nosniff`. Uploaded HTML is deliberately allowed to execute normally and no restrictive Content Security Policy is added. Treat all uploads as trusted content because they share the service's origin.

## Reverse Proxy

The proxy should stream request bodies rather than buffering them when possible. Configure its request-size limit to be at least `AGENTUP_MAX_UPLOAD_SIZE`. If `AGENTUP_URL` contains a path prefix, strip that prefix when forwarding to the backend; otherwise preserve request paths. TLS and network access controls belong at the proxy or VPN layer.

## Development

```sh
go test ./...
go vet ./...
golangci-lint run
go build ./cmd/agent-up ./cmd/agent-up-server
```

The project uses only the Go standard library. Nix exposes separate client and server packages:

```sh
nix build .#agent-up
nix build .#agent-up-server
```

The aliases `.#client` and `.#server` are also available. The default package is the `agent-up` client.
