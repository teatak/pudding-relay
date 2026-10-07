# pudding-relay

English · [简体中文](README.zh-CN.md)

A standalone, self-hosted Go relay for accessing Pudding Desktop from a phone or another computer through a browser outside the local network. Apache-2.0 licensed; no Cloudflare runtime dependency.

The server implements authenticated reverse tunnels, bounded REST/SSE forwarding, persistent desktop credential digests, and a lightweight English/Chinese admin interface. Pudding Desktop provides its own browser UI through the tunnel, along with pairing and business APIs. The relay image contains only the relay and its admin page; it builds independently of the desktop repository. Updating Pudding updates its remote UI without rebuilding or upgrading the relay.

## Setup flow

1. Install the HTTP backend using the command below, without a domain or certificate. It generates the admin secret and starts the relay. Your existing reverse proxy handles public HTTPS.
2. In Pudding **Settings → Remote access**, copy the desktop ID. Open your relay's `/admin`, register this ID, and copy the desktop credential that is shown once.
3. In Pudding Relay settings, enter the relay HTTPS origin and credential. Save and wait for **Connected**.
4. On the desktop, select **Generate authorization QR**. Scan it on the phone, or copy the authorization link into another computer's browser, then select **Connect**. No device-name entry or further desktop approval is needed. The code expires in five minutes and works once.

The desktop displays paired devices and pairing time. **Cancel pairing** immediately revokes browser access; it does not cancel an accepted task. Revoking a desktop credential in relay admin instead disconnects that desktop's entire tunnel.

One relay supports multiple Pudding desktops, each with its own ID, credential and tunnel. Multiple browsers can pair with each desktop. The desktop ID is stored in the desktop's local database and survives restarts, upgrades and IP address changes. A fresh data directory or deleting that database generates a new ID. Copying the database to another computer also copies the ID; the same ID cannot have two active tunnels.

## Connection modes and boundaries

Pudding supports two independently enabled entries; both may be active:

| Mode | Path |
| --- | --- |
| LAN Direct | Browser → desktop HTTP gateway → loopback daemon; bypasses this relay |
| Relay | Browser → public HTTPS relay → desktop gateway through an outbound WSS tunnel → loopback daemon |

LAN Direct belongs to Pudding Desktop. Both entries reuse the browser UI, desktop authorization codes, route authorization and business handlers. The daemon stays on loopback, and its startup token stays on the desktop. Users open an explicit endpoint; endpoints are not discovered or switched automatically. LAN and relay have separate browser logins under the same desktop authorization model. LAN uses HTTP on a trusted local network; relay continues to require public HTTPS/WSS. A LAN address change requires opening the new address and pairing again.

Business requests remain REST; session events remain SSE with `Last-Event-ID` resume. Conversations, tasks, approvals and files remain on the desktop. The relay persists only desktop IDs, labels, creation timestamps and SHA-256 credential digests; it does not persist conversation data or log tokens, cookies, request bodies or message contents. HTTPS/WSS protects each connection, not end-to-end encryption across the relay: users must trust its operator. Run exactly one relay instance with exclusive ownership of its registry file.

Browser HTML, scripts, styles and fonts are served by the selected desktop gateway. Different desktops on the same relay each provide their own UI version. The relay forwards response status, content type, caching and security headers from the desktop. If that desktop is offline, page and API requests return `503`; relay admin remains available.

Compatible browser builds provide conversations (including streaming, attachments, cancellation, user questions and approvals), Studio documents and tables, scheduled tasks, installed-app status, and authorized project files. Available operations follow the desktop gateway's authorization policy; native settings and system capabilities remain on the desktop. Full remote desktop control, voice and offline execution are not provided. Pudding must stay running; the selected entry must be reachable. LAN works independently of relay availability; models and tools may still require internet access.

## One-command installation

The public [Docker Hub image](https://hub.docker.com/r/teatak/pudding-relay) supports Linux amd64/arm64.

Install Docker Engine and Docker Compose v2, then run on the server:

```sh
mkdir -p pudding-relay
cd pudding-relay
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh | sh
```

Installation does not ask for a domain or HTTPS origin. Relay serves HTTP behind your existing HTTPS proxy. Successful installation prints the local HTTP admin URL, bind address and secret-file location, never the secret value. The image contains the relay and its admin page and supports Linux amd64/arm64.

For a noninteractive install, pass the settings directly:

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh \
  | env INSTALL_DIR=/opt/pudding-relay PORT=9623 sh
```

Installation defaults to the current directory; it does not create a `pudding-relay/` subdirectory. Installation creates `.env`, `compose.yaml`, `makefile` and `secrets/admin-secret`. The directory must be writable; system paths such as `/opt` require suitable permissions. Registrations live in the Compose named volume `relay_data`, outside the image. Reinstallation retains the secret, settings and registrations. Explicit environment arguments update their corresponding settings. `.env` is read as data, never executed as shell code.

| Parameter | Default / meaning |
| --- | --- |
| `INSTALL_DIR` | `$PWD`; reuse the same directory for reinstallation |
| `IMAGE` | `teatak/pudding-relay:latest`; a fixed tag/digest may be supplied |
| `PORT` | Host HTTP port, default `9623` |
| `BIND_ADDRESS` | `0.0.0.0` by default; set a specific host IP to restrict access |
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

The relay source and its admin page are Apache-2.0 licensed. The image includes the relay license and its dependency notice. Pudding browser assets remain in the desktop installation and are delivered through the tunnel.

## Local development

Requires Go 1.26+ and make. Generate an admin secret in a private file; do not put it in a command-line argument:

```sh
mkdir -p secrets
openssl rand -hex 32 > secrets/admin-secret
chmod 600 secrets/admin-secret
PUDDING_RELAY_ADMIN_SECRET_FILE="$PWD/secrets/admin-secret" \
make run
```

The default listener is `127.0.0.1:9623`; HTTP admin works directly in a trusted local environment. A reverse proxy terminates public HTTPS/WSS, and Relay stores no fixed external domain. Every admin API requires an explicit bearer credential (the administrator secret or a short-lived login token). Admin does not use cookie authentication or enable CORS; it does not infer authentication from Host, Origin or forwarding headers. Ordinary reverse-proxy configuration works with nonstandard external HTTPS ports, without a trusted-proxy setting or a custom admin location.

| Option | Meaning |
| --- | --- |
| `--listen` | HTTP listener, default `127.0.0.1:9623` |
| `--data-file` | Digest registry, default `data/registrations.json` |
| `PUDDING_RELAY_ADMIN_SECRET_FILE` | Required admin secret file, at least 32 bytes after trimming |

`make build` produces `bin/pudding-relay` and embeds the Git commit; the version comes from `VERSION`, and `COMMIT` can override commit metadata. `--version` prints it without requiring server configuration. SIGINT/SIGTERM closes tunnels, wakes active streams and shuts down HTTP. The registry is atomically replaced; POSIX systems use file mode `0600` and newly created directory mode `0700`. Windows uses the data directory’s ACL permissions. Back it up securely; lost credentials must be revoked and recreated.

## Docker and public HTTPS

Requires Docker Engine and Compose. Generate `secrets/admin-secret` as above. Docker bind-mounted secrets retain host file permissions; allow the non-root container to read the file while its host directory stays private:

```sh
chmod 700 secrets
chmod 444 secrets/admin-secret
```

Start the source HTTP backend:

```sh
docker compose up --build --detach --wait
curl --fail http://127.0.0.1:9623/healthz
docker compose down
```

This source Compose builds the relay image, publishes only host loopback, mounts the admin secret as a file and persists digests in `relay_data`. The image runs non-root with a read-only filesystem and dropped capabilities. User installs use the Docker Hub image above. Set `PUDDING_RELAY_PORT=18080` to change the loopback host port. Do not use `down --volumes` unless intentionally deleting all registrations.

Configure your proxy to reach the Relay HTTP backend. No trusted-proxy CIDRs or custom `/admin/api/` location are required. Desktop/browser pairing still validates the exact configured public origin.

The proxy must support WebSocket upgrade and unbuffered SSE, permit attachment sizes appropriate to your deployment, and avoid logging credential/cookie/body contents. Do not expose the relay's internal HTTP listener to the internet. Provision trusted HTTPS for real phones. Keep the admin secret file outside version control. To rotate it, replace the file and restart the relay. Desktop credentials are independently revoked in admin.

Browser UI deployment belongs to Pudding Desktop. The desktop gateway returns `/`, `/index.html`, `/pair` and `/s/{sessionID}` from its bundled browser build, substitutes the desktop base, and serves relative assets under `/d/{desktopID}/`. Every such request crosses the authenticated tunnel. Relay does not host, rewrite or cache a separate Pudding UI copy.

## Admin and endpoints

Open `/admin`, choose English or 简体中文, and enter the admin secret. At login, the page exchanges the secret for a random login token, clears the secret field, and keeps only that token in tab-scoped `sessionStorage`. Refreshing the tab restores login automatically. Tokens expire 24 hours after login; signing out revokes the token immediately, and restarting Relay invalidates all admin logins. Other tabs with independently created logins and desktop tunnels are unaffected. Browser tab restoration may restore tab storage, but never extends the server-enforced expiry. The application does not store the administrator secret in browser storage.

The standard login form supports browser password managers; saving is your choice and depends on browser settings. The password-manager account label is `admin@pudding-relay`, distinct from the generic `admin` used by other services; site addresses are managed separately by the browser. Browsers may still suggest credentials from related sites; verify the site/account before saving or updating a record.

Copy the desktop's existing ID from Pudding Remote access settings, register it, and copy the one-time credential into the desktop's relay settings. Do not invent a separate relay desktop ID. Revocation removes the stored digest, disconnects the tunnel and rejects future handshakes. Re-registering the same ID after revocation issues a new credential.

Desktop management APIs require `Authorization: Bearer <admin secret or login token>` on every operation. API automation can continue using the administrator secret directly. Only that secret can create a login token; login tokens cannot renew themselves or create other login tokens. Relay keeps login-token digests and expiration times only in memory, with at most 128 active logins. Expired entries are removed when another login is created. Cookies cannot authenticate admin; cross-origin preflight is not enabled. Host/Origin/proxy protocol rewriting does not reject a correctly authenticated admin request. Changing the proxy domain needs no Relay restart or configuration update. Pudding Desktop still needs its connection URL updated, and browsers pair again at the new origin. Responses use `Cache-Control: no-store`.

| Endpoint | Contract |
| --- | --- |
| `GET /healthz` | `{"status":"ok"}`, process health, not tunnel readiness; also HEAD |
| `GET /version` | Build `version` and `commit`; also HEAD |
| `POST /admin/api/session` | Administrator-secret bearer → `{"token":"…","expiresAt":"RFC3339"}`; 24-hour login; 429 when 128 active logins are reached |
| `DELETE /admin/api/session` | Login-token bearer → 204, revokes that login; already expired/revoked tokens also return 204 |
| `GET /admin/api/desktops` | `{"desktops":[{"desktopID":"…","label":"…","createdAt":"RFC3339","online":true}]}`; no credential/digest |
| `POST /admin/api/desktops` | JSON `{"desktopID":"existing-core-id","label":"My desktop"}` → 201 with `desktopID`, `label`, `createdAt`, one-time `token`; duplicate ID → 409 |
| `DELETE /admin/api/desktops/{desktopID}` | 204, persistent revocation and active tunnel close |
| `GET /tunnel` | WebSocket protocol v1, authenticated first frame |
| `/d/{desktopID}/*` | Forward pages, assets, pairing APIs and business REST/SSE through the registered desktop tunnel; offline → 503 |

The desktop gateway owns browser pairing/login and route authorization. The relay cannot bypass it and cannot proxy arbitrary destinations. Cookies, `Origin`, and `Last-Event-ID` are forwarded; authorization, host, hop-by-hop, forwarded and incoming `X-Pudding-*` headers are stripped. The relay only supplies `X-Pudding-Remote-Mode=relay`. Browser Origin is preserved; the desktop gateway validates pairing and business permissions against its configured relay URL.

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

Build directly from this repository:

```sh
make docker-build
```

`VERSION` is the single release-version source. `make docker-publish` publishes Linux amd64/arm64 images under both `latest` and the fixed-version tag. `IMAGE` changes the target reference; the Docker user needs push access. No desktop checkout, browser directory or extra build context is required. `docker build .` builds the same standalone image.

## Versioning and releases

Version 0.1.8 fixes the login-page flash when refreshing Relay admin. While restoring a login, the page shows only a centered spinner; the login form or management page appears after authentication completes. Upgrade the image using `make upgrade`; no proxy configuration change is needed.

`make build`, `make run` and Docker builds read the root `VERSION` file. Both `--version` and `/version` report that release version.

```sh
make version-patch   # 0.1.0 -> 0.1.1; update VERSION only
make version-minor   # 0.1.0 -> 0.2.0
make version-major   # 0.1.0 -> 1.0.0
make release-current # Publish the prepared, unreleased VERSION
make release         # Alias for release-patch
make release-patch
make release-minor
make release-major
```

Commit source on main and provide Docker Hub push access. The release script fetches main/tags, runs tests, commits a version bump if needed, pushes the multi-architecture image, then atomically pushes main and Git tag `vX.Y.Z`. The first patch release uses the prepared version; an already tagged current version increments patch. Published versions reject repeat publication. A failed image push creates no Git tag; retry retains the prepared version.

Install a fixed version:

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh \
  | env IMAGE=teatak/pudding-relay:0.1.8 sh
```

`latest` follows new releases. A fixed tag remains on that version; `make upgrade` keeps the image reference selected at installation.

## Verification

```sh
make fmt
make check
make build
make test-install
git diff --check
```

`make check` verifies formatting, runs vet and tests with the race detector. Tests cover digest persistence/restart, file permissions, concurrent registration, admin authentication, revoke/disconnect, protocol validation and closed admin CORS, bounded frames/streams, upload, SSE, trusted headers, cancellation, shutdown and desktop-owned browser pages/assets, HEAD, cache/security headers, multiple desktop UI versions and desktop-only UI updates. CI also builds and smoke-tests the container. Desktop/browser integration uses the browser build supplied by that desktop. Real-phone cellular access, sustained multi-device operation and capacity testing remain to be completed; the per-desktop stream limit is not a measured user-capacity figure.

## License

[Apache License 2.0](LICENSE).

## Upgrading from 0.1.0

Starting with 0.1.6, Relay forwards Pudding pages and assets through the desktop tunnel instead of shipping a browser build. Existing deployments need this one-time relay upgrade; subsequent Pudding UI updates only require updating Pudding. Desktop IDs, relay credentials and browser grants are preserved. Rerun the installer to remove the browser asset environment setting from its Compose template. Custom deployments must remove the old `--assets-dir` option, `PUDDING_RELAY_ASSETS_DIR` environment variable and browser asset mount.

Rerun the installer to remove obsolete `PUBLIC_URL` and Compose environment settings while retaining secrets and registration data; pulling an image alone does not update the old template. Starting with 0.1.3, the installer also removes obsolete `TRUSTED_PROXIES`. After updating the image, remove the custom admin proxy location previously required for nonstandard HTTPS ports. The removed `--public-url`, `--allow-insecure-loopback` and `--trusted-proxies` flags have no compatibility path.

Starting with `0.1.2`, the default service port is `9623` across Go, containers, health checks and fresh installations. Existing host `PORT` selections are retained; rerun the installer to update the Compose container target to `9623`. If an installation pins an older image tag, explicitly pass `IMAGE=teatak/pudding-relay:0.1.8` when rerunning.
