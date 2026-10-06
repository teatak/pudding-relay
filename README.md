# pudding-relay

English · [简体中文](README.zh-CN.md)

A standalone, self-hosted Go relay for accessing Pudding Desktop from a phone browser outside the local network. Apache-2.0 licensed; no Cloudflare runtime dependency.

The server implements authenticated reverse tunnels, bounded REST/SSE forwarding, persistent desktop credential digests, and a lightweight English/Chinese admin interface. A compatible Pudding Desktop gateway and the shared mobile Web build are required for phone access. The distribution image bundles matching browser assets, so users do not copy them manually. The Go server and its development image still build independently of the private desktop repository.

## Setup flow

1. Use the one-command installer below and prepare a reachable external HTTPS origin. It generates the admin secret and starts an image containing the browser UI.
2. In Pudding **Settings → Remote access**, copy the desktop ID. Open your relay's `/admin`, register this ID, and copy the desktop credential that is shown once.
3. In Pudding Relay settings, enter the relay HTTPS origin and credential. Save and wait for **Connected**.
4. On the desktop, select **Generate authorization QR**. Scan it on the phone and tap **Connect**. No device-name entry or further desktop approval is needed. The code expires in five minutes and works once.

The desktop displays paired devices and pairing time. **Cancel pairing** immediately revokes browser access; it does not cancel an accepted task. Revoking a desktop credential in relay admin instead disconnects that desktop's entire tunnel.

## Connection modes and boundaries

Pudding supports two independently enabled entries; both may be active:

| Mode | Path |
| --- | --- |
| LAN Direct | Phone → desktop HTTP gateway → loopback daemon; bypasses this relay |
| Relay | Phone → public HTTPS relay → desktop gateway through an outbound WSS tunnel → loopback daemon |

LAN Direct belongs to Pudding Desktop. Both entries reuse mobile Web, desktop authorization codes, route authorization and business handlers. The daemon stays on loopback, and its startup token stays on the desktop. Users open an explicit endpoint; the first release does not discover or switch endpoints automatically. LAN and relay have separate browser logins under the same desktop authorization model. LAN uses certificate-free HTTP on a trusted local network; relay continues to require public HTTPS/WSS. A LAN address change requires opening the new address and pairing again.

Business requests remain REST; session events remain SSE with `Last-Event-ID` resume. Conversations, tasks, approvals and files remain on the desktop. The relay persists only desktop IDs, labels, creation timestamps and SHA-256 credential digests; it does not persist conversation data or log tokens, cookies, request bodies or message contents. HTTPS/WSS protects each connection, not end-to-end encryption across the relay: users must trust its operator. Run exactly one relay instance with exclusive ownership of its registry file.

The mobile first release covers conversations, streaming, attachments, cancellation, user questions and Pudding approvals. Full remote desktop control, native system authorization, voice and offline execution are outside this release. Pudding must stay running; the selected entry must be reachable. LAN works independently of relay availability; models and tools may still require internet access.

## One-command installation

The public [Docker Hub image](https://hub.docker.com/r/teatak/pudding-relay) includes the browser UI and supports Linux amd64/arm64.

Install Docker Engine and Docker Compose v2, then run on the server:

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh | sh
```

The first run asks for the public HTTPS origin, such as `https://relay.example.com`. Relay itself serves HTTP behind your existing HTTPS proxy. Successful installation prints the admin URL and secret-file location, never the secret value. The image includes the shared browser UI and supports Linux amd64/arm64.

For a noninteractive install, pass the settings directly:

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh \
  | env PUBLIC_URL=https://relay.example.com INSTALL_DIR=/opt/pudding-relay PORT=8080 sh
```

The default directory is `pudding-relay/` under the current directory. Installation creates `.env`, `compose.yaml`, `makefile` and `secrets/admin-secret`. The directory must be writable; system paths such as `/opt` require suitable permissions. Registrations live in the Compose named volume `relay_data`, outside the image. Reinstallation retains the secret, settings and registrations. Explicit environment arguments update their corresponding settings. `.env` is read as data, never executed as shell code.

| Parameter | Default / meaning |
| --- | --- |
| `PUBLIC_URL` | Prompted initially; external HTTPS origin without path, query or fragment |
| `INSTALL_DIR` | `$PWD/pudding-relay`; reuse the same directory for reinstallation |
| `IMAGE` | `teatak/pudding-relay:latest`; a fixed tag/digest may be supplied |
| `PORT` | Host HTTP port, default `8080` |
| `BIND_ADDRESS` | `127.0.0.1` by default; select a reachable private host IP if needed |
| `NETWORK` | Optional existing Docker network; empty uses Compose's own network |

The following shortcuts require `make`; the corresponding `docker compose` commands also work directly in this directory.

Run from the installation directory:

```sh
make upgrade   # Pull/update, wait for readiness, retain data
make restart   # Restart
make stop      # Stop, retaining the data volume
make start     # Start or apply .env changes
make logs      # Follow logs
make status    # Show status
```

The installer does not configure DNS, TLS or the system Docker service. The external proxy must support WebSocket and unbuffered SSE. Do not add `--volumes` when stopping unless intentionally deleting registrations. Desktop remote access is not formally released; use a compatible desktop build containing this feature.

The Go relay source is Apache-2.0 licensed. Compiled Pudding browser assets in the image follow the Pudding Desktop license; that license and third-party notices are included, without publishing private desktop source.

## Local development

Requires Go 1.26+ and make. Generate an admin secret in a private file; do not put it in a command-line argument:

```sh
mkdir -p secrets
openssl rand -hex 32 > secrets/admin-secret
chmod 600 secrets/admin-secret
PUDDING_RELAY_ADMIN_SECRET_FILE="$PWD/secrets/admin-secret" \
PUDDING_RELAY_PUBLIC_URL=http://127.0.0.1:8080 \
go run ./cmd/pudding-relay --allow-insecure-loopback
```

HTTP is permitted only for an explicitly opted-in loopback test origin. Public deployment requires an HTTPS origin with no path, query, fragment or user information. The default listener is `127.0.0.1:8080`; a TLS reverse proxy terminates public HTTPS/WSS. `--public-url` or `PUDDING_RELAY_PUBLIC_URL` defines the trusted public origin; incoming forwarded headers cannot change it.

| Option | Meaning |
| --- | --- |
| `--listen` | HTTP listener, default `127.0.0.1:8080` |
| `--public-url` | Public HTTPS origin; environment: `PUDDING_RELAY_PUBLIC_URL` |
| `--data-file` | Digest registry, default `data/registrations.json` |
| `--assets-dir` | Shared mobile build directory; environment: `PUDDING_RELAY_ASSETS_DIR` |
| `--allow-insecure-loopback` | Explicit HTTP loopback test opt-in |
| `PUDDING_RELAY_ADMIN_SECRET_FILE` | Required admin secret file, at least 32 bytes after trimming |

`make build` produces `bin/pudding-relay` and embeds the Git commit; `VERSION` and `COMMIT` can override build metadata. `--version` prints it without requiring server configuration. SIGINT/SIGTERM closes tunnels, wakes active streams and shuts down HTTP. The registry is atomically replaced; POSIX systems use file mode `0600` and newly created directory mode `0700`. Windows uses the data directory’s ACL permissions. Back it up securely; lost credentials must be revoked and recreated.

## Docker and public HTTPS

Requires Docker Engine and Compose. Generate `secrets/admin-secret` as above. Docker bind-mounted secrets retain host file permissions; allow the non-root container to read the file while its host directory stays private:

```sh
chmod 700 secrets
chmod 444 secrets/admin-secret
```

Set your actual public origin:

```sh
PUDDING_RELAY_PUBLIC_URL=https://relay.example.com docker compose up --build --detach --wait
curl --fail http://127.0.0.1:8080/healthz
PUDDING_RELAY_PUBLIC_URL=https://relay.example.com docker compose down
```

This source Compose builds the `server` development target, publishes only host loopback, mounts the admin secret as a file and persists digests in `relay_data`. The image runs non-root with a read-only filesystem and dropped capabilities. User installs use the Docker Hub distribution above, which includes browser assets. Set `PUDDING_RELAY_PORT=18080` to change the loopback host port. Do not use `down --volumes` unless intentionally deleting all registrations.

Place a reverse proxy on the same host, for example Caddy:

```caddyfile
relay.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

The proxy must support WebSocket upgrade and unbuffered SSE, permit attachment sizes appropriate to your deployment, and avoid logging credential/cookie/body contents. Do not expose the relay's internal HTTP listener to the internet. Provision trusted HTTPS for real phones. Keep the admin secret file outside version control. To rotate it, replace the file and restart the relay. Desktop credentials are independently revoked in admin.

This feature has not been released; use a compatible desktop build. Its package includes the matching mobile assets: on macOS, `Pudding.app/Contents/Resources/app/web/dist/remote`; on Windows, `<installation directory>/resources/app/web/dist/remote`. Copy the directory contents into `mobile-dist` to deploy without private source access. Maintainers can also build `web/dist/remote` from the matching desktop source. The distribution target bundles these assets; this section is for source development and custom builds.

To install mobile assets, mount the shared build read-only and set `PUDDING_RELAY_ASSETS_DIR` to that container path, for example an override:

```yaml
services:
  relay:
    environment:
      PUDDING_RELAY_ASSETS_DIR: /assets
    volumes:
      - ./mobile-dist:/assets:ro
```

The shared HTML uses `<base href="__PUDDING_REMOTE_BASE__" />`; the relay replaces the marker with `/d/{desktopID}/` and permits that same-origin base in CSP. `/pair` and `/s/{sessionID}` deep links return this HTML, so relative assets resolve under the desktop base. They load while the desktop is offline; business requests return a clear `503`. Without `index.html`, the relay shows an explicit installation page, not a substitute mobile client.

## Admin and endpoints

Open `/admin`, choose English or 简体中文, and enter the admin secret. It stays in page memory and is not stored in browser storage. Copy the desktop's existing ID from Pudding Remote access settings, register it, and copy the one-time credential into the desktop's relay settings. Do not invent a separate relay desktop ID. Revocation removes the stored digest, disconnects the tunnel and rejects future handshakes. Re-registering the same ID after revocation issues a new credential.

Admin APIs require `Authorization: Bearer <admin secret>`; a supplied `Origin` must equal the configured public origin. They return `Cache-Control: no-store`.

| Endpoint | Contract |
| --- | --- |
| `GET /healthz` | `{"status":"ok"}`, process health, not tunnel readiness; also HEAD |
| `GET /version` | Build `version` and `commit`; also HEAD |
| `GET /admin/api/desktops` | `{"desktops":[{"desktopID":"…","label":"…","createdAt":"RFC3339","online":true}]}`; no credential/digest |
| `POST /admin/api/desktops` | JSON `{"desktopID":"existing-core-id","label":"My desktop"}` → 201 with `desktopID`, `label`, `createdAt`, one-time `token`; duplicate ID → 409 |
| `DELETE /admin/api/desktops/{desktopID}` | 204, persistent revocation and active tunnel close |
| `GET /tunnel` | WebSocket protocol v1, authenticated first frame |
| `/d/{desktopID}/api/*`, `/d/{desktopID}/remote/*` | HTTP forwarded through the registered desktop gateway; offline → 503 |
| `GET /d/{desktopID}/…` | Installed mobile assets; unknown desktop → 404 |

The desktop gateway owns phone pairing/login and route authorization. The relay cannot bypass it and cannot proxy arbitrary destinations. Cookies, `Origin`, and `Last-Event-ID` are forwarded; authorization, host, hop-by-hop, forwarded and incoming `X-Pudding-*` headers are stripped. The relay supplies `X-Pudding-Remote-Origin=<configured origin>` and `X-Pudding-Remote-Mode=relay` itself.

## Tunnel protocol v1

Connect outbound WSS to `/tunnel` using subprotocol `pudding-relay.v1`. Within five seconds send text JSON `{"type":"hello","protocol":1,"desktopID":"…","token":"…"}`; successful authentication returns `{"type":"hello","protocol":1,"desktopID":"…"}`. The token is never placed in the URL. One desktop may have one active tunnel. The relay sends a standard WebSocket Ping every 30 seconds; failure to receive Pong within 10 seconds closes the tunnel and wakes active HTTP streams. Native browser/Node WebSocket clients answer Pong automatically. Version mismatch or invalid credentials closes it.

Every subsequent frame has string `id`. The forwarded `path` preserves URL encoding, including `%3A` in canonical input-request IDs; encoded separators, double encoding, NUL and dot traversal are rejected:

| Direction | Frames |
| --- | --- |
| Relay → desktop | `request` with `method`, gateway-relative `path` (including query), `headers`; `request_data` with base64 `data`; `request_end` |
| Desktop → relay | `response` with `status` and `headers`; `response_data` with base64 `data`; `response_end` |
| Either | `cancel`; `ack` with `direction: "request"` or `"response"` |

Each decoded data chunk is at most 32,768 bytes; JSON frames are at most 65,536 bytes. Senders await one ACK after each chunk before sending the next chunk or stream end. Desktop ACKs after consuming request data; relay ACKs after writing/flushing HTTP response data. There are at most 64 active streams per desktop, one unacknowledged chunk per direction, and no conversation buffer or replay store. Cancellation, revocation, disconnect and shutdown wake waiting streams. Response status/headers arrive once before data. The tunnel carries REST/SSE; it does not change their business protocol.

## Building a distribution image

Maintainers supply the compiled shared browser directory and its matching notices, neither committed to this repository:

```sh
WEB_ASSETS_DIR=/path/to/pudding/web/dist/remote \
WEB_LEGAL_DIR=/path/to/pudding/dist/legal \
make docker-build
```

`make docker-publish` uses the same inputs to push Linux amd64/arm64 images. The default reference is `teatak/pudding-relay:latest`; `IMAGE` and `VERSION` override it. The current Docker user needs push access. `scripts/build-image.sh` checks Pudding's base marker and license files, then supplies binary assets through a named context, excluding private source and source maps.

The standalone Go image builds with `docker build --target server .`; the complete distribution needs the `browser` context. CI UI fixtures only test installation and persistence; they are never published as release assets.

## Verification

```sh
make fmt
make check
make build
make test-install
git diff --check
```

`make check` verifies formatting, runs vet and tests with the race detector. Tests cover digest persistence/restart, file permissions, concurrent registration, admin authentication, revoke/disconnect, protocol/public-origin validation, bounded frames/streams, upload, SSE, trusted headers, cancellation, shutdown and mobile deep links. CI also builds and smoke-tests the container. Desktop and real-phone integration must be tested with a compatible gateway and installed mobile build.

## License

[Apache License 2.0](LICENSE).
