# pudding-relay

English · [简体中文](README.zh-CN.md)

A self-hosted relay for accessing Pudding Desktop from a phone browser outside the local network.

**Status: repository bootstrap.** The runnable service currently provides health and build-version endpoints only. Desktop tunnels, authentication, pairing, the mobile web interface, and admin management are not implemented yet. Running this repository does not currently enable remote access to Pudding.

## Planned architecture

The phone connects to the public relay over HTTPS. Pudding Desktop initiates an outbound WSS tunnel to the relay and forwards authorized requests to its loopback daemon.

- Business requests remain REST; session events remain SSE with resumable event IDs.
- The desktop owns conversations, tasks, approvals, and files. The relay does not persist their contents.
- The daemon startup token stays on the desktop.
- A standalone Go service and Docker deployment have no Cloudflare runtime dependency.
- Initial deployment targets one relay instance, with a lightweight admin interface.
- User documentation and the future admin interface support English and Simplified Chinese.

The planned first mobile release focuses on conversations, streamed results, attachments, cancellation, user questions, and Pudding approvals. Full remote desktop control, native system authorization, voice, and offline execution are outside that first release. The computer must stay online with Pudding running.

HTTPS/WSS protects each network connection; it does not provide end-to-end encryption across the relay. Users must trust their relay operator. Authentication and authorization must be implemented before business traffic is exposed.

## Run locally

Requires Go 1.26 or later and make.

```sh
git clone https://github.com/teatak/pudding-relay.git
cd pudding-relay
make run
```

The default listener is `127.0.0.1:8080`.

```sh
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/version
```

| Endpoint | Current response |
| --- | --- |
| `GET /healthz` | `{"status":"ok"}`; process health only, not tunnel readiness |
| `GET /version` | Build version and commit; defaults to `dev` and `unknown` for `go run` |

Both endpoints also support HEAD. Unknown routes return 404; unsupported methods on these routes return 405.

Build and run a binary:

```sh
make build
./bin/pudding-relay --version
./bin/pudding-relay --listen=127.0.0.1:8080
```

`make build` embeds the Git commit. Set `VERSION` and `COMMIT` explicitly when needed. SIGINT and SIGTERM trigger graceful shutdown.

## Run with Docker

Requires Docker Engine and Docker Compose.

```sh
docker compose up --build --detach --wait
curl --fail http://127.0.0.1:8080/healthz
docker compose logs
docker compose down
```

Compose builds a local image; no prebuilt registry image is published by this bootstrap. The process runs as a non-root user with a read-only filesystem, dropped capabilities, and a container health check. The container listens on port 8080; Compose publishes it only on the host loopback interface.

To avoid a host port conflict:

```sh
PUDDING_RELAY_PORT=18080 docker compose up --build --detach --wait
curl --fail http://127.0.0.1:18080/healthz
PUDDING_RELAY_PORT=18080 docker compose down
```

The current service has no persistent data or data volume. Future public deployment will require HTTPS, authenticated desktop registration, phone pairing, bounded streaming, and relay storage. This Compose file is a bootstrap smoke-test setup, not a completed public relay deployment.

## Development

```sh
make fmt
make check
make build
git diff --check
```

`make check` checks formatting, runs go vet, and executes tests with the race detector. CI runs these checks and builds and smoke-tests the Docker service.

| Path | Purpose |
| --- | --- |
| `cmd/pudding-relay/` | Process entry point, flags, and shutdown |
| `internal/httpserver/` | HTTP handler and contract tests |
| `Dockerfile`, `compose.yaml` | Container build and local deployment |
| `.github/workflows/ci.yml` | Go checks and Docker smoke test |
| `AGENTS.md` | Contribution and implementation boundaries |

Next milestones are the authenticated reverse tunnel, desktop integration and pairing, the shared mobile web entry, and lightweight admin management. Protocol and product behavior will be documented alongside their implementations. The distribution of shared browser assets must be defined before they are bundled; building the open-source relay must not require access to a private desktop repository.

## License

[Apache License 2.0](LICENSE).
